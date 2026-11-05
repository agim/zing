package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/lidzatest"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"zing/internal/atview"
	"zing/internal/chat"
	"zing/internal/moderation"
	"zing/internal/providers"
	"zing/internal/social"
	"zing/schema"
)

func hostedFixture(t *testing.T, s *lidzatest.Server, owner string) string {
	t.Helper()
	did := "did:plc:" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
	_, e := db.From(s.Context()).Exec(s.Context(), `INSERT INTO hosted_identity(owner_id,network,username,server,password_sealed,subject,handle,state) VALUES($1,'bluesky',$2,'http://127.0.0.1:2583','sealed',$3,$2||'.test','active')`, owner, "test"+strings.ReplaceAll(uuid.NewString(), "-", "")[:16], did)
	if e != nil {
		t.Fatal(e)
	}
	return did
}
func TestLocalChatConsentIsolationEncryptionAndDeletion(t *testing.T) {
	s := setup(t, nil)
	ctx := s.Context()
	a, ta := account(t, s, true)
	b, _ := account(t, s, true)
	c, tc := account(t, s, true)
	da := hostedFixture(t, s, a)
	dbb := hostedFixture(t, s, b)
	dc := hostedFixture(t, s, c)
	conversation, e := chat.Open(ctx, a, da, dbb)
	if e != nil {
		t.Fatal(e)
	}
	if !conversation.CanReply || !conversation.Pending || conversation.Service != "zing" {
		t.Fatalf("invitation %#v", conversation)
	}
	again, e := chat.Open(ctx, b, dbb, da)
	if e != nil || again.ID != conversation.ID || !again.CanAccept || again.CanReply {
		t.Fatalf("recipient consent %#v %v", again, e)
	}
	send := schema.MessageInput{ConversationID: conversation.ID, Text: "A private invitation", ClientID: uuid.NewString()}
	if e = chat.Send(ctx, a, da, send); e != nil {
		t.Fatal(e)
	}
	if e = chat.Send(ctx, a, da, send); e != nil {
		t.Fatal("idempotent retry", e)
	}
	conflicting := send
	conflicting.Text = "different"
	if e = chat.Send(ctx, a, da, conflicting); e == nil {
		t.Fatal("reused identifier accepted different text")
	}
	second := send
	second.ClientID = uuid.NewString()
	if e = chat.Send(ctx, a, da, second); e == nil {
		t.Fatal("inviter bypassed acceptance")
	}
	if e = chat.Accept(ctx, a, da, conversation.ID); e == nil {
		t.Fatal("inviter accepted own invitation")
	}
	if _, e = chat.Messages(ctx, c, dc, conversation.ID, ""); e == nil {
		t.Fatal("nonparticipant read messages")
	}
	if e = chat.Accept(ctx, c, dc, conversation.ID); e == nil {
		t.Fatal("nonparticipant accepted")
	}
	if e = chat.Send(ctx, c, dc, send); e == nil {
		t.Fatal("nonparticipant sent")
	}
	messages, e := chat.Messages(ctx, b, dbb, conversation.ID, "")
	if e != nil || len(messages.Items) != 1 || !messages.CanAccept || messages.CanReply {
		t.Fatalf("recipient read %#v %v", messages, e)
	}
	t.Setenv("ZING_PDS_URL", "http://127.0.0.1:2583")
	expiry := time.Now().Add(time.Hour)
	if e = social.SaveConnection(ctx, a, providers.Session{ExpiresAt: &expiry, Network: "bluesky", Hosted: true, Server: "http://127.0.0.1:2583", Subject: da, Handle: "fixture.test", Access: "fixture-token"}); e != nil {
		t.Fatal(e)
	}
	var connectionID string
	if e = db.From(ctx).QueryRow(ctx, `SELECT id::text FROM social_connection WHERE owner_id=$1 AND subject=$2`, a, da).Scan(&connectionID); e != nil {
		t.Fatal(e)
	}
	if _, e = db.From(ctx).Exec(ctx, `UPDATE social_connection SET hosted=true WHERE owner_id=$1 AND id::text=$2`, a, connectionID); e != nil {
		t.Fatal(e)
	}
	for _, suffix := range []string{"Id", "ID"} {
		var rendered schema.MessageList
		path := "/api/v1/messages?connection" + suffix + "=" + connectionID + "&conversation" + suffix + "=" + conversation.ID
		if response := s.JSON(t, "GET", path, nil, &rendered, lidzatest.Bearer(ta)); response.StatusCode != 200 || len(rendered.Items) != 1 || rendered.Items[0].Text != send.Text {
			t.Fatalf("message client query %s: %d %#v", suffix, response.StatusCode, rendered)
		}
		if response := s.JSON(t, "GET", path, nil, nil, lidzatest.Bearer(tc)); response.StatusCode != 404 {
			t.Fatal("cross-user query alias", response.StatusCode)
		}
	}
	var profile schema.SocialAccount
	if response := s.JSON(t, "GET", "/api/v1/profile?connectionId="+connectionID+"&id="+url.QueryEscape(dbb), nil, &profile, lidzatest.Bearer(ta)); response.StatusCode != 200 || profile.ID != dbb {
		t.Fatal("profile client query", response.StatusCode, profile.ID)
	}
	var sealed string
	if e = db.From(ctx).QueryRow(ctx, `SELECT text_sealed FROM chat_message WHERE conversation_id::text=$1`, conversation.ID).Scan(&sealed); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(sealed, send.Text) {
		t.Fatal("plaintext chat stored")
	}
	ev, e := chat.Evidence(ctx, b, dbb, messages.Items[0].ID)
	if e != nil || ev.Text != send.Text || ev.Owner != a {
		t.Fatal("selected evidence", ev, e)
	}
	if _, e = chat.Evidence(ctx, c, dc, messages.Items[0].ID); e == nil {
		t.Fatal("nonparticipant obtained report evidence")
	}
	if e = chat.Accept(ctx, b, dbb, conversation.ID); e != nil {
		t.Fatal(e)
	}
	if e = chat.Send(ctx, b, dbb, schema.MessageInput{ConversationID: conversation.ID, Text: "Accepted reply", ClientID: uuid.NewString()}); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 55; i++ {
		if e = chat.Send(ctx, a, da, schema.MessageInput{ConversationID: conversation.ID, Text: fmt.Sprintf("page %d", i), ClientID: uuid.NewString()}); e != nil {
			t.Fatal(e)
		}
	}
	first, e := chat.Messages(ctx, b, dbb, conversation.ID, "")
	if e != nil || len(first.Items) != 50 || first.Cursor == "" {
		t.Fatal("message page", len(first.Items), e)
	}
	older, e := chat.Messages(ctx, b, dbb, conversation.ID, first.Cursor)
	if e != nil || len(older.Items) != 7 || older.Cursor != "" {
		t.Fatal("older page", len(older.Items), e)
	}
	ids := map[string]bool{}
	for _, m := range append(first.Items, older.Items...) {
		if ids[m.ID] {
			t.Fatal("duplicate message page")
		}
		ids[m.ID] = true
	}
	if _, e = chat.Messages(ctx, b, dbb, conversation.ID, uuid.NewString()); e == nil {
		t.Fatal("foreign cursor accepted")
	}
	_, e = db.From(ctx).Exec(ctx, `INSERT INTO moderation_account(owner_id,suspended) VALUES($1,true)`, a)
	if e != nil {
		t.Fatal(e)
	}
	if e = chat.Send(ctx, b, dbb, schema.MessageInput{ConversationID: conversation.ID, Text: "blocked", ClientID: uuid.NewString()}); e == nil {
		t.Fatal("suspended participant accepted")
	}
	if e = moderation.Check(ctx, a); e == nil {
		t.Fatal("suspension ignored")
	}
	_, e = db.From(ctx).Exec(ctx, `DELETE FROM moderation_account WHERE owner_id=$1`, a)
	if e != nil {
		t.Fatal(e)
	}
	_, e = moderation.CreateReport(ctx, b, providers.Session{Hosted: true, Network: "bluesky", Subject: dbb}, schema.ReportRequest{Kind: "message", Target: da, MessageID: &messages.Items[0].ID, Reason: "Explicit selected-message fixture"})
	if e != nil {
		t.Fatal(e)
	}
	tx, e := db.From(ctx).Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if e = moderation.DeleteOwner(ctx, tx, a); e != nil {
		t.Fatal(e)
	}
	if e = chat.DeleteOwner(ctx, tx, a); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	var evidenceLeft int
	if e = db.From(ctx).QueryRow(ctx, `SELECT count(*) FROM moderation_report WHERE reporter_id=$1 AND evidence_sealed<>''`, b).Scan(&evidenceLeft); e != nil || evidenceLeft != 0 {
		t.Fatal("deleted target evidence retained", evidenceLeft, e)
	}
	remaining, e := chat.Messages(ctx, b, dbb, conversation.ID, "")
	if e != nil || len(remaining.Items) != 1 || remaining.Items[0].Text != "Accepted reply" || remaining.CanReply {
		t.Fatalf("deleted sender data retained %#v %v", remaining, e)
	}
}
func TestHostedFeedIndexPaginationFollowingMuteAndModeration(t *testing.T) {
	s := setup(t, nil)
	ctx := s.Context()
	a, _ := account(t, s, true)
	b, _ := account(t, s, true)
	da := hostedFixture(t, s, a)
	dbb := hostedFixture(t, s, b)
	// Source records are generated by a native PDS-shaped fixture; writes remain
	// source-authoritative and every indexed record preserves its URI and CID.
	const cid = "bafyreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku"
	now := time.Now().UTC().Add(-time.Hour)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/xrpc/com.atproto.repo.listRecords") {
			t.Errorf("unexpected source %s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		collection := r.URL.Query().Get("collection")
		records := []map[string]any{}
		if collection == "app.bsky.feed.post" {
			for i := 0; i < 26; i++ {
				records = append(records, map[string]any{"uri": fmt.Sprintf("at://%s/%s/%s", dbb, collection, syntax.NewTIDFromTime(now.Add(time.Duration(i)*time.Second), 0)), "cid": cid, "value": map[string]any{"$type": collection, "text": fmt.Sprintf("Indexed post %d", i), "createdAt": now.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano)}})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"records": records})
	}))
	defer source.Close()
	session := providers.Session{Hosted: true, Network: "bluesky", Subject: dbb, Server: source.URL, Client: source.Client()}
	if e := atview.Refresh(ctx, session); e != nil {
		t.Fatal(e)
	}
	reader := atview.Reader{Owner: a, DID: da, Server: source.URL}
	f, e := reader.Feed(ctx, "", "")
	if e != nil || len(f.Items) != 0 {
		t.Fatal("unfollowed author leaked into Following", len(f.Items), e)
	}
	reader.Scope = "discover"
	f, e = reader.Feed(ctx, "", "")
	if e != nil || len(f.Items) != 20 || f.Cursor == "" {
		t.Fatal("Discover first page", len(f.Items), e)
	}
	next, e := reader.Feed(ctx, f.Cursor, "")
	if e != nil || len(next.Items) != 6 {
		t.Fatal("Discover next page", len(next.Items), e)
	}
	if next.Items[0].ID == f.Items[0].ID {
		t.Fatal("pagination repeated")
	}
	wrong := reader
	wrong.DID = dbb
	if _, e = wrong.Feed(ctx, f.Cursor, ""); e == nil {
		t.Fatal("cross-identity cursor accepted")
	}
	if e = atview.Mute(ctx, a, da, dbb, true); e != nil {
		t.Fatal(e)
	}
	f, e = reader.Feed(ctx, "", "")
	if e != nil || len(f.Items) != 0 {
		t.Fatal("muted posts visible", e)
	}
	if e = atview.Mute(ctx, a, da, dbb, false); e != nil {
		t.Fatal(e)
	}
	_, e = db.From(ctx).Exec(ctx, `INSERT INTO moderation_action(moderator_id,kind,network,target,reason,state) VALUES($1,'remove_post','bluesky',$2,'Native retry fixture','failed')`, a, next.Items[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = reader.Post(ctx, next.Items[0].ID); e == nil {
		t.Fatal("failed native moderation exposed a locally removed post")
	}
	_, e = db.From(ctx).Exec(ctx, `INSERT INTO moderation_account(owner_id,suspended) VALUES($1,true)`, b)
	if e != nil {
		t.Fatal(e)
	}
	f, e = reader.Feed(ctx, "", "")
	if e != nil || len(f.Items) != 0 {
		t.Fatal("suspended posts visible", e)
	}
	tx, e := db.From(ctx).Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if e = atview.DeleteOwner(ctx, tx, b); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = db.From(ctx).QueryRow(ctx, `SELECT count(*) FROM at_record WHERE did=$1`, dbb).Scan(&count); e != nil || count != 0 {
		t.Fatal("indexed deleted data", count, e)
	}
}
func TestSuspendedUserCanAppealButCannotPublish(t *testing.T) {
	s := setup(t, nil)
	owner, token := account(t, s, true)
	if _, e := db.From(s.Context()).Exec(s.Context(), `INSERT INTO moderation_account(owner_id,suspended) VALUES($1,true)`, owner); e != nil {
		t.Fatal(e)
	}
	if r := s.JSON(t, "GET", "/api/v1/connections", nil, nil, lidzatest.Bearer(token)); r.StatusCode != 403 {
		t.Fatal("suspended access", r.StatusCode)
	}
	for range 2 {
		if r := s.JSON(t, "POST", "/api/v1/moderation/appeal", map[string]string{"reason": "Please review my suspension"}, nil, lidzatest.Bearer(token)); r.StatusCode != 200 {
			t.Fatal("appeal", r.StatusCode)
		}
	}
	var count int
	if e := db.From(s.Context()).QueryRow(context.Background(), `SELECT count(*) FROM moderation_report WHERE reporter_id=$1 AND kind='appeal'`, owner).Scan(&count); e != nil || count != 1 {
		t.Fatal("duplicate appeal", count, e)
	}
	if len(social.ConnectionCapabilities("bluesky", true)) == 0 {
		t.Fatal("hosted capabilities absent")
	}
}

func TestModerationRequiresOperatorAndPreservesPartialFailure(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	var apCalls atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/account-state" {
			if r.Header.Get("Authorization") != "Bearer fixture-agent" {
				t.Error("missing agent authentication")
			}
			apCalls.Add(1)
			w.Write([]byte(`{"ok":true}`))
			return
		}
		if r.URL.Path == "/xrpc/com.atproto.admin.updateSubjectStatus" {
			u, p, ok := r.BasicAuth()
			if !ok || u != "admin" || p != "fixture-admin" {
				t.Error("missing native admin authentication")
			}
			if fail.Load() {
				w.WriteHeader(503)
				w.Write([]byte(`{"error":"private upstream detail"}`))
				return
			}
			w.Write([]byte(`{}`))
			return
		}
		t.Errorf("unexpected backend %s", r.URL.Path)
		w.WriteHeader(500)
	}))
	defer backend.Close()
	t.Setenv("ZING_ACTIVITYPUB_URL", backend.URL)
	t.Setenv("ZING_PDS_URL", backend.URL)
	t.Setenv("ZING_ACTIVITYPUB_AGENT_URL", backend.URL)
	t.Setenv("ZING_ACTIVITYPUB_AGENT_TOKEN", "fixture-agent")
	t.Setenv("ZING_PDS_ADMIN_PASSWORD", "fixture-admin")
	s := setup(t, nil)
	staff, staffToken := account(t, s, true)
	owner, userToken := account(t, s, true)
	did := hostedFixture(t, s, owner)
	t.Setenv("ADMIN_USERS", staff)
	_, e := db.From(s.Context()).Exec(s.Context(), `UPDATE hosted_identity SET server=$1 WHERE subject=$2`, backend.URL, did)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.From(s.Context()).Exec(s.Context(), `INSERT INTO hosted_identity(owner_id,network,username,server,subject,password_sealed,state) VALUES($1,'mastodon','moderationfixture',$2,'native-account','sealed','active')`, owner, backend.URL)
	if e != nil {
		t.Fatal(e)
	}
	key := uuid.NewString()
	form := url.Values{"kind": {"suspend"}, "target": {owner}, "reason": {"Fixture safety action"}, "requestKey": {key}}
	submit := func(token string) int {
		req, err := http.NewRequest("POST", s.URL+"/admin/moderation/action", strings.NewReader(form.Encode()))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Origin", s.URL)
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		return res.StatusCode
	}
	if status := submit(userToken); status != 403 {
		t.Fatal("ordinary user reached moderation", status)
	}
	// Framework admin allowlists deliberately cache for three seconds.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if status := submit(staffToken); status == 303 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("operator not admitted")
		}
		time.Sleep(100 * time.Millisecond)
	}
	var id, state string
	for i := 0; i < 100; i++ {
		e = db.From(s.Context()).QueryRow(s.Context(), `SELECT id::text,state FROM moderation_action WHERE request_key=$1`, key).Scan(&id, &state)
		if e == nil && state == "failed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if e != nil || state != "failed" {
		t.Fatal("backend failure was hidden", state, e)
	}
	if e = moderation.Check(s.Context(), owner); e == nil {
		t.Fatal("native partial failure cleared immediate suspension")
	}
	var destinations int
	if e = db.From(s.Context()).QueryRow(s.Context(), `SELECT count(*) FROM moderation_destination WHERE action_id::text=$1`, id).Scan(&destinations); e != nil || destinations != 2 {
		t.Fatal("per-network checkpoints absent", destinations, e)
	}
	fail.Store(false)
	for i := 0; i < 150; i++ {
		if e = db.From(s.Context()).QueryRow(s.Context(), `SELECT state FROM moderation_action WHERE id::text=$1`, id).Scan(&state); e != nil {
			t.Fatal(e)
		}
		if state == "applied" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if state != "applied" {
		t.Fatal("native retry did not recover", state)
	}
	if apCalls.Load() != 1 {
		t.Fatal("confirmed destination repeated", apCalls.Load())
	}
	if status := submit(staffToken); status != 303 {
		t.Fatal("duplicate request failed", status)
	}
	var count int
	if e = db.From(s.Context()).QueryRow(s.Context(), `SELECT count(*) FROM moderation_action WHERE request_key=$1`, key).Scan(&count); e != nil || count != 1 {
		t.Fatal("duplicate moderation action", count, e)
	}
}
