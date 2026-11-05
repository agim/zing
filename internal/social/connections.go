package social

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/credentials"
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	"zing/internal/providers"
	"zing/schema"
)

func Owner(ctx context.Context) string { return auth.CurrentUser(ctx).ID }
func Capabilities(n string) []string {
	switch n {
	case "mastodon":
		return []string{"feed", "search", "profile", "follow", "like", "repost", "reply", "publish", "images", "notifications", "conversations", "mute", "block"}
	case "bluesky":
		return []string{"feed", "search", "profile", "follow", "like", "repost", "reply", "publish", "images", "notifications", "conversations", "mute", "block"}
	case "threads":
		return []string{"own-posts", "profile", "publish", "reply"}
	}
	return []string{}
}
func Seal(value string) (string, error) {
	key, e := credentials.Key(".")
	if e != nil {
		return "", e
	}
	return credentials.Encrypt(key, []byte(value))
}
func Open(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	key, e := credentials.Key(".")
	if e != nil {
		return "", e
	}
	v, e := credentials.Decrypt(key, value)
	return string(v), e
}
func randomState() (string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func SaveConnection(ctx context.Context, owner string, s providers.Session) error {
	a, e := Seal(s.Access)
	if e != nil {
		return e
	}
	r, e := Seal(s.Refresh)
	if e != nil {
		return e
	}
	_, e = db.From(ctx).Exec(ctx, `INSERT INTO social_connection(owner_id,network,server,subject,handle,access_sealed,refresh_sealed,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(owner_id,network,subject) DO UPDATE SET server=excluded.server,handle=excluded.handle,access_sealed=excluded.access_sealed,refresh_sealed=excluded.refresh_sealed,expires_at=excluded.expires_at,reconnect=false`, owner, s.Network, s.Server, s.Subject, s.Handle, a, r, s.ExpiresAt)
	return e
}
func Connections(ctx context.Context, req *router.Request[router.None]) (schema.ConnectionList, error) {
	out := schema.ConnectionList{Items: []schema.ConnectionView{}}
	rows, e := db.From(ctx).Query(ctx, `SELECT id::text,network,server,subject,handle,reconnect,hosted FROM social_connection WHERE owner_id=$1 ORDER BY created_at`, Owner(ctx))
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var c schema.ConnectionView
		if e = rows.Scan(&c.ID, &c.Network, &c.Server, &c.Subject, &c.Handle, &c.Reconnect, &c.Hosted); e != nil {
			return out, e
		}
		c.Capabilities = ConnectionCapabilities(c.Network, c.Hosted)
		out.Items = append(out.Items, c)
	}
	return out, rows.Err()
}
func Load(ctx context.Context, owner, id string) (providers.Session, error) {
	var s providers.Session
	var a, r string
	e := db.From(ctx).QueryRow(ctx, `SELECT network,server,subject,handle,access_sealed,refresh_sealed,expires_at,hosted FROM social_connection WHERE owner_id=$1 AND id::text=$2 AND NOT reconnect`, owner, id).Scan(&s.Network, &s.Server, &s.Subject, &s.Handle, &a, &r, &s.ExpiresAt, &s.Hosted)
	if errors.Is(e, pgx.ErrNoRows) {
		return s, router.NotFound("Connection")
	}
	if e != nil {
		return s, e
	}
	if s.Hosted {
		s.Client, e = providers.HostedClient(s.Server)
		if e != nil {
			return s, e
		}
		if s.Network == "bluesky" {
			values, readErr := env.Values(".")
			if readErr != nil {
				return s, readErr
			}
			s.LocalView = values["ZING_PDS_READ_MODE"] != "appview"
			rows, err := db.From(ctx).Query(ctx, `SELECT subject,handle FROM hosted_identity WHERE network='bluesky' AND state='active' AND server=$1 ORDER BY created_at LIMIT 100`, s.Server)
			if err != nil {
				return s, err
			}
			for rows.Next() {
				var actor providers.HostedActor
				if err = rows.Scan(&actor.DID, &actor.Handle); err != nil {
					rows.Close()
					return s, err
				}
				s.Actors = append(s.Actors, actor)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return s, err
			}
		}
	}
	s.Access, e = Open(a)
	if e != nil {
		return s, e
	}
	s.Refresh, e = Open(r)
	if e != nil {
		return s, e
	}
	if s.Network == "bluesky" && strings.HasPrefix(s.Access, "oauth:") {
		s.Auth = oauthRequest(owner, s.Subject, strings.TrimPrefix(s.Access, "oauth:"))
		return s, nil
	}
	if s.Network == "bluesky" && s.ExpiresAt != nil && s.ExpiresAt.Before(time.Now().Add(time.Minute)) {
		refresh := s
		refresh.Access = s.Refresh
		var reply struct {
			AccessJwt  string `json:"accessJwt"`
			RefreshJwt string `json:"refreshJwt"`
		}
		if e = refresh.Do(ctx, "POST", "/xrpc/com.atproto.server.refreshSession", nil, &reply); e != nil {
			_, markErr := db.From(ctx).Exec(ctx, `UPDATE social_connection SET reconnect=true WHERE owner_id=$1 AND id::text=$2`, owner, id)
			if markErr != nil {
				return s, markErr
			}
			return s, e
		}
		s.Access, s.Refresh = reply.AccessJwt, reply.RefreshJwt
		t := time.Now().Add(90 * time.Minute)
		s.ExpiresAt = &t
		e = SaveConnection(ctx, owner, s)
	}
	if s.Network == "threads" && s.ExpiresAt != nil && s.ExpiresAt.Before(time.Now().Add(24*time.Hour)) {
		var tok struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int    `json:"expires_in"`
		}
		if e = s.Do(ctx, "GET", "/refresh_access_token?grant_type=th_refresh_token", nil, &tok); e != nil {
			return s, e
		}
		s.Access = tok.AccessToken
		t := time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
		s.ExpiresAt = &t
		e = SaveConnection(ctx, owner, s)
	}
	return s, e
}
func Disconnect(ctx context.Context, req *router.Request[router.None]) (router.None, error) {
	tx, e := db.From(ctx).Begin(ctx)
	if e != nil {
		return router.None{}, e
	}
	defer tx.Rollback(ctx)
	_, e = tx.Exec(ctx, `UPDATE delivery SET state='cancelled',updated_at=now() WHERE owner_id=$1 AND connection_id::text=$2 AND state IN ('pending','failed')`, Owner(ctx), req.Param("id"))
	if e != nil {
		return router.None{}, e
	}
	var subject, network string
	e = tx.QueryRow(ctx, `SELECT subject,network FROM social_connection WHERE owner_id=$1 AND id::text=$2`, Owner(ctx), req.Param("id")).Scan(&subject, &network)
	if errors.Is(e, pgx.ErrNoRows) {
		return router.None{}, router.NotFound("Connection")
	}
	if e != nil {
		return router.None{}, e
	}
	var hosted bool
	if e = tx.QueryRow(ctx, `SELECT hosted FROM social_connection WHERE owner_id=$1 AND id::text=$2`, Owner(ctx), req.Param("id")).Scan(&hosted); e != nil {
		return router.None{}, e
	}
	if hosted {
		return router.None{}, router.Errorf(409, "This identity is hosted here. It cannot be disconnected like an external account.")
	}
	if network == "bluesky" {
		if _, e = tx.Exec(ctx, `DELETE FROM o_auth_data WHERE owner_id=$1 AND starts_with(id,$2)`, Owner(ctx), Owner(ctx)+":session:"+subject+":"); e != nil {
			return router.None{}, e
		}
	}
	tag, e := tx.Exec(ctx, `DELETE FROM social_connection WHERE owner_id=$1 AND id::text=$2`, Owner(ctx), req.Param("id"))
	if e != nil {
		return router.None{}, e
	}
	if tag.RowsAffected() == 0 {
		return router.None{}, router.NotFound("Connection")
	}
	return router.None{}, tx.Commit(ctx)
}
func Connect(ctx context.Context, req *router.Request[schema.ConnectionRequest]) (schema.ConnectResult, error) {
	var in schema.ConnectionInput
	if err := Convert(req.Body, &in); err != nil {
		return schema.ConnectResult{}, err
	}
	result := schema.ConnectResult{}
	var count int
	if err := db.From(ctx).QueryRow(ctx, `SELECT count(*) FROM social_connection WHERE owner_id=$1`, Owner(ctx)).Scan(&count); err != nil {
		return schema.ConnectResult{}, err
	}
	if count >= 20 {
		return schema.ConnectResult{}, router.Errorf(422, "Connect at most twenty accounts")
	}
	if in.Network == "bluesky" {
		if in.Handle != "" && in.AppPassword == "" {
			return connectBlueskyOAuth(ctx, req, in.Handle)
		}
		if in.Handle == "" || in.AppPassword == "" {
			return result, router.Errorf(422, "Provide your Bluesky handle and an app password, not your main password")
		}
		server := "https://bsky.social"
		var e error
		if in.Server != "" {
			server, e = providers.Server(in.Server)
			if e != nil {
				return result, e
			}
		}
		s := providers.Session{Network: "bluesky", Server: server}
		var reply struct {
			Did        string `json:"did"`
			Handle     string `json:"handle"`
			AccessJwt  string `json:"accessJwt"`
			RefreshJwt string `json:"refreshJwt"`
		}
		e = s.Do(ctx, "POST", "/xrpc/com.atproto.server.createSession", map[string]string{"identifier": in.Handle, "password": in.AppPassword}, &reply)
		if e != nil {
			return result, e
		}
		s.Subject, s.Handle, s.Access, s.Refresh = reply.Did, reply.Handle, reply.AccessJwt, reply.RefreshJwt
		t := time.Now().Add(90 * time.Minute)
		s.ExpiresAt = &t
		return schema.ConnectResult{URL: "/#Settings"}, SaveConnection(ctx, Owner(ctx), s)
	}
	state, e := randomState()
	if e != nil {
		return result, e
	}
	callback := strings.TrimRight(setting("APP_URL"), "/") + "/api/v1/connections/callback"
	if !strings.HasPrefix(callback, "http") {
		return result, router.Errorf(503, "APP_URL must be configured for OAuth callbacks")
	}
	var server, client, secret, authURL string
	switch in.Network {
	case "mastodon":
		server, e = providers.Server(in.Server)
		if e != nil {
			return result, e
		}
		var app struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		}
		e = (providers.Session{Server: server}).Do(ctx, "POST", "/api/v1/apps", url.Values{"client_name": {"Zing"}, "redirect_uris": {callback}, "scopes": {"read write follow"}, "website": {setting("APP_URL")}}, &app)
		if e != nil {
			return result, e
		}
		client, secret = app.ClientID, app.ClientSecret
		authURL = server + "/oauth/authorize?" + url.Values{"response_type": {"code"}, "client_id": {client}, "redirect_uri": {callback}, "scope": {"read write follow"}, "state": {state}}.Encode()
	case "threads":
		server = "https://graph.threads.net"
		client, secret = setting("THREADS_CLIENT_ID"), setting("THREADS_CLIENT_SECRET")
		if client == "" || secret == "" {
			return result, router.Errorf(503, "Threads developer app is not configured. Public access also requires Meta approval.")
		}
		authURL = "https://threads.net/oauth/authorize?" + url.Values{"client_id": {client}, "redirect_uri": {callback}, "scope": {"threads_basic,threads_content_publish,threads_read_replies,threads_manage_replies"}, "response_type": {"code"}, "state": {state}}.Encode()
	default:
		return result, router.Errorf(422, "Unknown network")
	}
	sealed, e := Seal(secret)
	if e != nil {
		return result, e
	}
	_, e = db.From(ctx).Exec(ctx, `INSERT INTO o_auth_attempt(id,owner_id,network,server,client_id,client_secret_sealed,expires_at) VALUES($1,$2,$3,$4,$5,$6,now()+interval '10 minutes')`, state, Owner(ctx), in.Network, server, client, sealed)
	if e != nil {
		return result, e
	}
	req.SetCookie(&http.Cookie{Name: "zing_connect", Value: state, Path: "/api/v1/connections/callback", HttpOnly: true, Secure: strings.HasPrefix(callback, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: 600})
	return schema.ConnectResult{URL: authURL}, nil
}
func Callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	fail := func() { http.Redirect(w, r, "/#Settings?connect_error=failed", http.StatusSeeOther) }
	cookie, e := r.Cookie("zing_connect")
	if e != nil || cookie.Value != r.URL.Query().Get("state") || cookie.Value == "" {
		fail()
		return
	}
	// A browser-bound state is consumed once before exchanging the code.
	var owner, n, server, client, seal string
	e = db.From(ctx).QueryRow(ctx, `DELETE FROM o_auth_attempt WHERE id=$1 AND expires_at>now() RETURNING owner_id,network,server,client_id,client_secret_sealed`, cookie.Value).Scan(&owner, &n, &server, &client, &seal)
	if e != nil {
		fail()
		return
	}
	secret, e := Open(seal)
	if e != nil {
		fail()
		return
	}
	callback := strings.TrimRight(setting("APP_URL"), "/") + "/api/v1/connections/callback"
	path := "/oauth/token"
	if n == "threads" {
		path = "/oauth/access_token"
	}
	s := providers.Session{Network: n, Server: server}
	var tok struct {
		AccessToken string          `json:"access_token"`
		UserID      json.RawMessage `json:"user_id"`
	}
	e = s.Do(ctx, "POST", path, url.Values{"client_id": {client}, "client_secret": {secret}, "redirect_uri": {callback}, "grant_type": {"authorization_code"}, "code": {r.URL.Query().Get("code")}}, &tok)
	if e != nil {
		fail()
		return
	}
	s.Access = tok.AccessToken
	if n == "threads" {
		var long struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int    `json:"expires_in"`
		}
		if e = s.Do(ctx, "GET", "/access_token?"+url.Values{"grant_type": {"th_exchange_token"}, "client_secret": {secret}}.Encode(), nil, &long); e != nil {
			fail()
			return
		}
		s.Access = long.AccessToken
		t := time.Now().Add(time.Duration(long.ExpiresIn) * time.Second)
		s.ExpiresAt = &t
	}

	var profile struct {
		ID       string `json:"id"`
		Acct     string `json:"acct"`
		Username string `json:"username"`
	}
	path = "/api/v1/accounts/verify_credentials"
	if n == "threads" {
		path = "/v1.0/me?fields=id,username"
	}
	if e = s.Do(ctx, "GET", path, nil, &profile); e != nil {
		fail()
		return
	}
	s.Subject, s.Handle = profile.ID, profile.Acct
	if s.Handle == "" {
		s.Handle = profile.Username
	}
	if e = SaveConnection(ctx, owner, s); e != nil {
		fail()
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "zing_connect", Value: "", Path: "/api/v1/connections/callback", HttpOnly: true, MaxAge: -1})
	http.Redirect(w, r, "/#Settings", http.StatusSeeOther)
}

// Convert normalizes optional wire values to the durable record shape.
func Convert(in, out any) error {
	b, e := json.Marshal(in)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, out)
}

func setting(name string) string {
	values, err := env.Values(".")
	if err != nil {
		return ""
	}
	return values[name]
}

func ConnectionCapabilities(network string, hosted bool) []string {
	values, readErr := env.Values(".")
	if hosted && network == "bluesky" && (readErr != nil || values["ZING_PDS_READ_MODE"] != "appview") {
		return []string{"feed", "search", "profile", "follow", "like", "repost", "reply", "publish", "images", "block"}
	}
	return Capabilities(network)
}
