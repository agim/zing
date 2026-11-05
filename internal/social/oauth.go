package social

import (
	"context"
	"encoding/json"
	"net/http"

	"strings"
	"time"

	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"zing/internal/providers"
	"zing/schema"
)

// bskyStore seals the complete DPoP session, including its private key.
// Requests are consumed atomically, sessions are scoped to the Zing owner.
type bskyStore struct{ Owner, LastState string }

func (s *bskyStore) save(ctx context.Context, id string, value any, ttl time.Duration, upsert bool) error {
	b, e := json.Marshal(value)
	if e != nil {
		return e
	}
	sealed, e := Seal(string(b))
	if e != nil {
		return e
	}
	sql := `INSERT INTO o_auth_data(id,owner_id,sealed,expires_at) VALUES($1,$2,$3,$4)`
	if upsert {
		sql += ` ON CONFLICT(id) DO UPDATE SET sealed=excluded.sealed,expires_at=excluded.expires_at WHERE o_auth_data.owner_id=excluded.owner_id`
	}
	_, e = db.From(ctx).Exec(ctx, sql, s.Owner+":"+id, s.Owner, sealed, time.Now().Add(ttl))
	return e
}
func (s *bskyStore) load(ctx context.Context, id string, out any, consume bool) error {
	var sealed string
	sql := `SELECT sealed FROM o_auth_data WHERE owner_id=$1 AND id=$2 AND expires_at>now()`
	if consume {
		sql = `DELETE FROM o_auth_data WHERE owner_id=$1 AND id=$2 AND expires_at>now() RETURNING sealed`
	}
	if e := db.From(ctx).QueryRow(ctx, sql, s.Owner, s.Owner+":"+id).Scan(&sealed); e != nil {
		return e
	}
	raw, e := Open(sealed)
	if e != nil {
		return e
	}
	return json.Unmarshal([]byte(raw), out)
}
func (s *bskyStore) GetSession(ctx context.Context, did syntax.DID, id string) (*oauth.ClientSessionData, error) {
	var d oauth.ClientSessionData
	e := s.load(ctx, "session:"+did.String()+":"+id, &d, false)
	return &d, e
}
func (s *bskyStore) SaveSession(ctx context.Context, d oauth.ClientSessionData) error {
	return s.save(ctx, "session:"+d.AccountDID.String()+":"+d.SessionID, d, 180*24*time.Hour, true)
}
func (s *bskyStore) DeleteSession(ctx context.Context, did syntax.DID, id string) error {
	_, e := db.From(ctx).Exec(ctx, `DELETE FROM o_auth_data WHERE owner_id=$1 AND id=$2`, s.Owner, s.Owner+":session:"+did.String()+":"+id)
	return e
}
func (s *bskyStore) GetAuthRequestInfo(ctx context.Context, state string) (*oauth.AuthRequestData, error) {
	var d oauth.AuthRequestData
	e := s.load(ctx, "request:"+state, &d, true)
	return &d, e
}
func (s *bskyStore) SaveAuthRequestInfo(ctx context.Context, d oauth.AuthRequestData) error {
	s.LastState = d.State
	return s.save(ctx, "request:"+d.State, d, 10*time.Minute, false)
}
func (s *bskyStore) DeleteAuthRequestInfo(ctx context.Context, state string) error {
	_, e := db.From(ctx).Exec(ctx, `DELETE FROM o_auth_data WHERE owner_id=$1 AND id=$2`, s.Owner, s.Owner+":request:"+state)
	return e
}
func bskyApp(store *bskyStore) (*oauth.ClientApp, error) {
	base := strings.TrimRight(setting("APP_URL"), "/")
	if !strings.HasPrefix(base, "https://") {
		return nil, router.Errorf(503, "Bluesky OAuth requires a public HTTPS APP_URL. Use an app password for local development.")
	}
	config := oauth.NewPublicConfig(base+"/oauth-client-metadata.json", base+"/api/v1/connections/bluesky/callback", []string{"atproto", "transition:generic", "transition:chat.bsky"})
	app := oauth.NewClientApp(&config, store)
	app.Client = providers.Client()
	return app, nil
}
func BlueskyMetadata(w http.ResponseWriter, r *http.Request) {
	app, e := bskyApp(&bskyStore{})
	if e != nil {
		router.WriteError(w, r, e)
		return
	}
	router.JSON(w, http.StatusOK, app.Config.ClientMetadata())
}
func connectBlueskyOAuth(ctx context.Context, req *router.Request[schema.ConnectionRequest], handle string) (schema.ConnectResult, error) {
	store := &bskyStore{Owner: Owner(ctx)}
	app, e := bskyApp(store)
	if e != nil {
		return schema.ConnectResult{}, e
	}
	redirect, e := app.StartAuthFlow(ctx, handle)
	if e != nil {
		return schema.ConnectResult{}, router.Errorf(422, "Could not start Bluesky authorization. Check the handle and try again.")
	}
	if store.LastState == "" {
		return schema.ConnectResult{}, router.Errorf(502, "Bluesky authorization state was not saved")
	}
	// Confirm persistence because the upstream helper does not propagate a store error.
	var exists bool
	e = db.From(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM o_auth_data WHERE owner_id=$1 AND id=$2)`, Owner(ctx), Owner(ctx)+":request:"+store.LastState).Scan(&exists)
	if e != nil {
		return schema.ConnectResult{}, e
	}
	if !exists {
		return schema.ConnectResult{}, router.Errorf(503, "Could not persist authorization")
	}
	req.SetCookie(&http.Cookie{Name: "zing_bsky_connect", Value: store.LastState, Path: "/api/v1/connections/bluesky/callback", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	return schema.ConnectResult{URL: redirect}, nil
}
func BlueskyCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	fail := func() { http.Redirect(w, r, "/#Settings?connect_error=bluesky", http.StatusSeeOther) }
	c, e := r.Cookie("zing_bsky_connect")
	if e != nil || c.Value == "" || c.Value != r.URL.Query().Get("state") {
		fail()
		return
	}
	var owner string
	e = db.From(ctx).QueryRow(ctx, `SELECT owner_id FROM o_auth_data WHERE id=owner_id || ':request:' || $1 AND expires_at>now()`, c.Value).Scan(&owner)
	if e != nil {
		fail()
		return
	}
	store := &bskyStore{Owner: owner}
	app, e := bskyApp(store)
	if e != nil {
		fail()
		return
	}
	data, e := app.ProcessCallback(ctx, r.URL.Query())
	if e != nil {
		fail()
		return
	}
	server, e := providers.Server(data.HostURL)
	if e != nil {
		fail()
		return
	}
	s := providers.Session{Network: "bluesky", Server: server, Subject: data.AccountDID.String(), Handle: data.AccountDID.String(), Access: "oauth:" + data.SessionID}
	if e = SaveConnection(ctx, owner, s); e != nil {
		fail()
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "zing_bsky_connect", Value: "", Path: "/api/v1/connections/bluesky/callback", HttpOnly: true, Secure: true, MaxAge: -1})
	http.Redirect(w, r, "/#Settings", http.StatusSeeOther)
}
func oauthRequest(owner, did, id string) func(*http.Request) (*http.Response, error) {
	return func(req *http.Request) (*http.Response, error) {
		ctx := req.Context()
		conn, e := db.From(ctx).Acquire(ctx)
		if e != nil {
			return nil, e
		}
		defer conn.Release()
		lock := "oauth:" + owner + ":" + did
		var locked bool
		if e = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, lock).Scan(&locked); e != nil {
			return nil, e
		}
		if !locked {
			return nil, router.Errorf(429, "Another request is refreshing this account. Try again shortly.")
		}
		defer func() {
			c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := conn.Exec(c, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, lock); err != nil {
				conn.Conn().Close(c)
			}
		}()
		app, e := bskyApp(&bskyStore{Owner: owner})
		if e != nil {
			return nil, e
		}
		d, e := syntax.ParseDID(did)
		if e != nil {
			return nil, e
		}
		sess, e := app.ResumeSession(ctx, d, id)
		if e != nil {
			return nil, e
		}
		req.Header.Del("Authorization")
		endpoint := strings.TrimPrefix(req.URL.Path, "/xrpc/")
		return sess.DoWithAuth(providers.Client(), req, syntax.NSID(endpoint))
	}
}
