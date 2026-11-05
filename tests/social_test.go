package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/lidzatest"
	"github.com/google/uuid"
	"zing/app"
	"zing/internal/providers"
	"zing/internal/social"
	"zing/schema"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func reply(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func setup(t *testing.T, rt http.RoundTripper) *lidzatest.Server {
	t.Helper()
	t.Setenv("LIDZA_MASTER_KEY", strings.Repeat("ab", 32))
	t.Setenv("MAIL_PROVIDER", "outbox")
	t.Setenv("CACHE_URL", "memory")
	t.Setenv("STORAGE_DIR", t.TempDir())
	if rt == nil {
		rt = transport(func(*http.Request) (*http.Response, error) { return nil, errors.New("unexpected network call") })
	}
	return lidzatest.Start(t, app.New(nil), lidzatest.WithTransport(rt))
}
func account(t *testing.T, s *lidzatest.Server, verified bool) (string, string) {
	t.Helper()
	ctx := s.Context()
	a := auth.From(ctx)
	p, e := a.CreateUser(ctx, uuid.NewString()+"@zing.example", "Test person", "test-password-123")
	if e != nil {
		t.Fatal(e)
	}
	if verified {
		if e = a.MarkVerified(ctx, p.Subject); e != nil {
			t.Fatal(e)
		}
	}
	tokens, e := a.Login(ctx, p.Subject, nil)
	if e != nil {
		t.Fatal(e)
	}
	return p.Subject, tokens.Access
}
func connect(t *testing.T, s *lidzatest.Server, owner, network string) string {
	t.Helper()
	if e := social.SaveConnection(s.Context(), owner, providers.Session{Network: network, Server: "https://social.example", Subject: "did:plc:" + uuid.NewString(), Handle: "person.social", Access: "fixture-token", Refresh: "fixture-refresh"}); e != nil {
		t.Fatal(e)
	}
	var id string
	if e := db.From(s.Context()).QueryRow(s.Context(), `SELECT id::text FROM social_connection WHERE owner_id=$1 ORDER BY created_at DESC LIMIT 1`, owner).Scan(&id); e != nil {
		t.Fatal(e)
	}
	return id
}
func TestAuthorizationOwnershipAndMedia(t *testing.T) {
	s := setup(t, nil)
	owner, token := account(t, s, true)
	_, other := account(t, s, true)
	_, unverified := account(t, s, false)
	if res := s.JSON(t, "GET", "/api/v1/connections", nil, nil); res.StatusCode != 401 {
		t.Fatalf("anonymous %d", res.StatusCode)
	}
	if res := s.JSON(t, "GET", "/api/v1/connections", nil, nil, lidzatest.Bearer(unverified)); res.StatusCode != 403 {
		t.Fatalf("unverified %d", res.StatusCode)
	}
	id := connect(t, s, owner, "mastodon")
	var list schema.ConnectionList
	s.JSON(t, "GET", "/api/v1/connections", nil, &list, lidzatest.Bearer(other))
	if len(list.Items) != 0 {
		t.Fatal("another user's connections exposed")
	}
	if res := s.JSON(t, "DELETE", "/api/v1/connections/"+id, nil, nil, lidzatest.Bearer(other)); res.StatusCode != 404 {
		t.Fatalf("cross-user deletion %d", res.StatusCode)
	}
	var sealed string
	if e := db.From(s.Context()).QueryRow(s.Context(), `SELECT access_sealed FROM social_connection WHERE owner_id=$1`, owner).Scan(&sealed); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(sealed, "fixture-token") {
		t.Fatal("credential stored in plaintext")
	}
	// Real private media upload; another owner cannot fetch it.
	data := []byte{137, 80, 78, 71, 13, 10, 26, 10}
	data = append(data, make([]byte, 512)...)
	req, e := http.NewRequest("PUT", s.URL+"/api/v1/media", bytes.NewReader(data))
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "image/png")
	res, e := s.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	var media schema.MediaView
	if e = json.NewDecoder(res.Body).Decode(&media); e != nil {
		t.Fatal(e)
	}
	if res.StatusCode != 200 {
		t.Fatalf("upload %d", res.StatusCode)
	}
	if r := s.JSON(t, "GET", "/api/v1/media/"+media.ID, nil, nil, lidzatest.Bearer(other)); r.StatusCode != 404 {
		t.Fatalf("cross-user media %d", r.StatusCode)
	}
}
func TestDraftSchedulingCancellationAndIdempotence(t *testing.T) {
	s := setup(t, nil)
	owner, token := account(t, s, true)
	_, other := account(t, s, true)
	id := connect(t, s, owner, "bluesky")
	when := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	in := map[string]any{"text": "Scheduled from Zing", "destinations": []string{id}, "mediaIds": []string{}, "timezone": "America/New_York", "scheduledAt": when}
	var d schema.DraftView
	if r := s.JSON(t, "POST", "/api/v1/drafts", in, &d, lidzatest.Bearer(token)); r.StatusCode != 200 {
		t.Fatalf("save %d", r.StatusCode)
	}
	if r := s.JSON(t, "POST", "/api/v1/drafts/"+d.ID+"/publish", nil, nil, lidzatest.Bearer(other)); r.StatusCode != 404 {
		t.Fatalf("cross-user publish %d", r.StatusCode)
	}
	for i := 0; i < 2; i++ {
		if r := s.JSON(t, "POST", "/api/v1/drafts/"+d.ID+"/publish", nil, nil, lidzatest.Bearer(token)); r.StatusCode != 200 {
			t.Fatalf("publish %d", r.StatusCode)
		}
	}
	var count int
	if e := db.From(s.Context()).QueryRow(s.Context(), `SELECT count(*) FROM delivery WHERE owner_id=$1 AND draft_id::text=$2`, owner, d.ID).Scan(&count); e != nil {
		t.Fatal(e)
	}
	if count != 1 {
		t.Fatalf("duplicate destinations %d", count)
	}
	var jobs int
	if e := db.From(s.Context()).QueryRow(s.Context(), `SELECT count(*) FROM job WHERE kind='zing.publish' AND payload->>'Owner'=$1`, owner).Scan(&jobs); e != nil {
		t.Fatal(e)
	}
	if jobs != 1 {
		t.Fatalf("duplicate jobs %d", jobs)
	}
	s.JSON(t, "POST", "/api/v1/drafts/"+d.ID+"/cancel", nil, nil, lidzatest.Bearer(token))
	var deliveries schema.DeliveryList
	s.JSON(t, "GET", "/api/v1/deliveries", nil, &deliveries, lidzatest.Bearer(token))
	if len(deliveries.Items) != 1 || deliveries.Items[0].State != "cancelled" {
		t.Fatalf("not cancelled %+v", deliveries)
	}
}
func TestUncertainPublicationNeverAutomaticallyRetries(t *testing.T) {
	var writes atomic.Int32
	rt := transport(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" && r.URL.Path == "/api/v1/statuses" {
			writes.Add(1)
			return nil, errors.New("connection lost after sending request")
		}
		return reply(`{}`), nil
	})
	s := setup(t, rt)
	owner, token := account(t, s, true)
	id := connect(t, s, owner, "mastodon")
	var d schema.DraftView
	s.JSON(t, "POST", "/api/v1/drafts", map[string]any{"text": "Do not duplicate", "destinations": []string{id}, "mediaIds": []string{}, "timezone": "UTC"}, &d, lidzatest.Bearer(token))
	s.JSON(t, "POST", "/api/v1/drafts/"+d.ID+"/publish", nil, nil, lidzatest.Bearer(token))
	var delivery schema.DeliveryView
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var list schema.DeliveryList
		s.JSON(t, "GET", "/api/v1/deliveries", nil, &list, lidzatest.Bearer(token))
		if len(list.Items) > 0 && list.Items[0].State == "uncertain" {
			delivery = list.Items[0]
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if delivery.State != "uncertain" || writes.Load() != 1 {
		t.Fatalf("uncertain outcome %+v calls=%d", delivery, writes.Load())
	}
	if r := s.JSON(t, "POST", "/api/v1/deliveries/"+delivery.ID+"/retry", nil, nil, lidzatest.Bearer(token)); r.StatusCode != 409 {
		t.Fatalf("unsafe retry accepted: %d", r.StatusCode)
	}
}
func TestAccountDeletionRemovesApplicationRecords(t *testing.T) {
	s := setup(t, nil)
	owner, token := account(t, s, true)
	connect(t, s, owner, "mastodon")
	res := s.JSON(t, "POST", "/api/v1/auth/delete", map[string]string{"password": "test-password-123"}, nil, lidzatest.Bearer(token))
	if res.StatusCode != 204 {
		t.Fatalf("delete %d", res.StatusCode)
	}
	var count int
	if e := db.From(s.Context()).QueryRow(context.Background(), `SELECT count(*) FROM social_connection WHERE owner_id=$1`, owner).Scan(&count); e != nil || count != 0 {
		t.Fatalf("credentials survived: %d %v", count, e)
	}
}

func TestRelayRequiresOwnedConfirmedParents(t *testing.T) {
	s := setup(t, transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/xrpc/app.bsky.feed.getPosts" {
			return reply(`{"posts":[{"record":{}}]}`), nil
		}
		return nil, errors.New("unexpected external request")
	}))
	owner, token := account(t, s, true)
	a := connect(t, s, owner, "mastodon")
	b := connect(t, s, owner, "bluesky")
	ctx := s.Context()
	var parent string
	e := db.From(ctx).QueryRow(ctx, `INSERT INTO draft(owner_id,text,destinations,media_ids,status) VALUES($1,'Original','[]','[]','published') RETURNING id::text`, owner).Scan(&parent)
	if e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{a, b} {
		if _, e = db.From(ctx).Exec(ctx, `INSERT INTO delivery(owner_id,draft_id,connection_id,state,remote_id,remote_cid) VALUES($1,$2,$3,'published','parent-id','parent-cid')`, owner, parent, id); e != nil {
			t.Fatal(e)
		}
	}
	input := map[string]any{"text": "Explicit reply relay", "destinations": []string{a, b}, "mediaIds": []string{}, "timezone": "UTC", "relayParentDraftId": parent, "replyId": "source-parent", "replyConnectionId": a}
	var saved schema.DraftView
	if r := s.JSON(t, "POST", "/api/v1/drafts", input, &saved, lidzatest.Bearer(token)); r.StatusCode != 200 {
		t.Fatalf("mapped relay rejected %d", r.StatusCode)
	}
	otherOwner, otherToken := account(t, s, true)
	c := connect(t, s, otherOwner, "mastodon")
	input["destinations"] = []string{c}
	input["replyConnectionId"] = c
	if r := s.JSON(t, "POST", "/api/v1/drafts", input, nil, lidzatest.Bearer(otherToken)); r.StatusCode != 422 {
		t.Fatalf("another user's relay map was accepted: %d", r.StatusCode)
	}
}
func TestSuccessfulDestinationsAreNotRequeued(t *testing.T) {
	var writes atomic.Int32
	s := setup(t, transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/v1/statuses" {
			writes.Add(1)
			if r.Header.Get("Idempotency-Key") == "" {
				t.Error("missing idempotency key")
			}
			return reply(`{"id":"confirmed-post","url":"https://social.example/posts/confirmed-post"}`), nil
		}
		return reply(`{}`), nil
	}))
	owner, token := account(t, s, true)
	id := connect(t, s, owner, "mastodon")
	var draft schema.DraftView
	s.JSON(t, "POST", "/api/v1/drafts", map[string]any{"text": "Publish once", "destinations": []string{id}, "mediaIds": []string{}, "timezone": "UTC"}, &draft, lidzatest.Bearer(token))
	s.JSON(t, "POST", "/api/v1/drafts/"+draft.ID+"/publish", nil, nil, lidzatest.Bearer(token))
	var found schema.DeliveryView
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var list schema.DeliveryList
		s.JSON(t, "GET", "/api/v1/deliveries", nil, &list, lidzatest.Bearer(token))
		if len(list.Items) > 0 && list.Items[0].State == "published" {
			found = list.Items[0]
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if found.State != "published" {
		t.Fatalf("publication did not complete %+v", found)
	}
	if r := s.JSON(t, "POST", "/api/v1/deliveries/"+found.ID+"/retry", nil, nil, lidzatest.Bearer(token)); r.StatusCode != 409 {
		t.Fatalf("successful destination retried %d", r.StatusCode)
	}
	s.JSON(t, "POST", "/api/v1/drafts/"+draft.ID+"/publish", nil, nil, lidzatest.Bearer(token))
	if writes.Load() != 1 {
		t.Fatalf("duplicate publications %d", writes.Load())
	}
}
