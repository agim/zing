package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"zing/schema"
)

func TestPublicDestinations(t *testing.T) {
	for _, v := range []string{"127.0.0.1", "::1", "10.0.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "::ffff:127.0.0.1", "64:ff9b::7f00:1", "2001:db8::1", "224.0.0.1"} {
		if PublicIP(netip.MustParseAddr(v)) {
			t.Errorf("accepted unsafe destination %s", v)
		}
	}
	if !PublicIP(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("public address refused")
	}
	for _, v := range []string{"http://host", "https://user:pass@host", "https://host/path", "https://host:8080", "https://host?x=1"} {
		if _, e := Server(v); e == nil {
			t.Errorf("accepted invalid server %s", v)
		}
	}
}
func TestMastodonFeedAndActions(t *testing.T) {
	var actions []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing auth")
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/timelines") {
			io.WriteString(w, `[{"id":"p1","content":"<p>Hello <strong>world</strong></p><script>alert(1)</script>","account":{"id":"a1","acct":"person@social.example","display_name":"Person"},"media_attachments":[],"visibility":"public"}]`)
			return
		}
		actions = append(actions, r.URL.Path)
		io.WriteString(w, `{}`)
	}))
	defer server.Close()
	s := Session{Network: "mastodon", Server: server.URL, Access: "test-token", Client: server.Client()}
	f, e := s.Feed(context.Background(), "", "")
	if e != nil || len(f.Items) != 1 {
		t.Fatalf("feed %+v %v", f, e)
	}
	if strings.Contains(f.Items[0].Text, "<") || f.Items[0].Author.ID != "a1" {
		t.Fatal("raw remote HTML or identity loss")
	}
	if e = s.Action(context.Background(), schema.ActionInput{Action: "like", Target: "p1"}); e != nil {
		t.Fatal(e)
	}
	if len(actions) != 1 || actions[0] != "/api/v1/statuses/p1/favourite" {
		t.Fatalf("wrong native action %v", actions)
	}
}
func TestBlueskyUnlikeOnlyDeletesOwnedRecord(t *testing.T) {
	var deleted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			io.WriteString(w, `{"posts":[{"viewer":{"like":"at://did:plc:me/app.bsky.feed.like/like-key"}}]}`)
			return
		}
		var body map[string]string
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Fatal(e)
		}
		if body["repo"] != "did:plc:me" || body["collection"] != "app.bsky.feed.like" || body["rkey"] != "like-key" {
			t.Errorf("wrong deletion %+v", body)
		}
		deleted = true
		io.WriteString(w, `{}`)
	}))
	defer server.Close()
	s := Session{Network: "bluesky", Subject: "did:plc:me", Server: server.URL, Client: server.Client()}
	if e := s.Action(context.Background(), schema.ActionInput{Action: "unlike", Target: "at://did:plc:them/app.bsky.feed.post/p"}); e != nil {
		t.Fatal(e)
	}
	if !deleted {
		t.Fatal("unlike did not delete the original like record")
	}
}
func TestNoRedirectAndErrorsDoNotLeakTokens(t *testing.T) {
	s := Session{Server: "https://127.0.0.1", Access: "do-not-leak"}
	e := s.Do(context.Background(), "GET", "/secret?access_token=do-not-leak", nil, nil)
	if e == nil || strings.Contains(e.Error(), "do-not-leak") {
		t.Fatalf("request should fail without credentials in error: %v", e)
	}
}

func TestThreadsImagesUseExplicitPrivateObjectLinks(t *testing.T) {
	var created, published bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := r.ParseForm(); e != nil {
			t.Fatal(e)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/threads"):
			if r.Form.Get("media_type") != "IMAGE" || r.Form.Get("image_url") != "https://objects.example/signed-image" || r.Form.Get("alt_text") != "A garden" {
				t.Errorf("incorrect image container %+v", r.Form)
			}
			created = true
			io.WriteString(w, `{"id":"container"}`)
		case strings.HasSuffix(r.URL.Path, "/threads_publish"):
			if r.Form.Get("creation_id") != "container" {
				t.Error("wrong container")
			}
			published = true
			io.WriteString(w, `{"id":"post"}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	s := Session{Network: "threads", Subject: "me", Server: server.URL, Client: server.Client()}
	ref, _, e := s.Publish(context.Background(), "Image post", "key", nil, []Image{{SourceURL: "https://objects.example/signed-image", Alt: "A garden"}})
	if e != nil || ref.URI != "post" || !created || !published {
		t.Fatalf("image publication %v %+v %v %v", e, ref, created, published)
	}
}
