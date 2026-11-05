// Package hosting provisions identities on this installation's real servers.
package hosting

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	"zing/internal/providers"
	"zing/internal/social"
	"zing/schema"
)

type rejectedName struct{}

func (rejectedName) Error() string {
	return "That username is unavailable on the social server. Choose another name."
}

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,29}$`)

func configuration(network string) (map[string]string, string, error) {
	v, e := env.Values(".")
	if e != nil {
		return nil, "", e
	}
	key := "ZING_ACTIVITYPUB_URL"
	if network == "bluesky" {
		key = "ZING_PDS_URL"
	} else if network != "mastodon" {
		return nil, "", router.Errorf(422, "Choose ActivityPub or Bluesky hosting")
	}
	origin := strings.TrimRight(v[key], "/")
	if origin == "" {
		return nil, "", router.Errorf(503, "This hosted network is not configured")
	}
	if _, e = providers.HostedClient(origin); e != nil {
		return nil, "", router.Errorf(503, "Hosted backend configuration is invalid")
	}
	if network == "mastodon" && (v["ZING_ACTIVITYPUB_AGENT_URL"] == "" || v["ZING_ACTIVITYPUB_AGENT_TOKEN"] == "") {
		return nil, "", router.Errorf(503, "ActivityPub provisioning is not configured")
	}
	if network == "bluesky" && (v["ZING_PDS_ADMIN_PASSWORD"] == "" || v["ZING_PDS_HANDLE_DOMAIN"] == "") {
		return nil, "", router.Errorf(503, "PDS provisioning is not configured")
	}
	return v, origin, nil
}
func Hosting(ctx context.Context, _ *router.Request[router.None]) (schema.HostingStatus, error) {
	out := schema.HostingStatus{Networks: []schema.HostedNetworkView{}, Identities: []schema.HostedIdentityView{}}
	for _, n := range []string{"mastodon", "bluesky"} {
		v, origin, e := configuration(n)
		domain := ""
		if e == nil {
			u, parseErr := url.Parse(origin)
			if parseErr != nil {
				return out, parseErr
			}
			domain = u.Host
			if n == "bluesky" {
				domain = v["ZING_PDS_HANDLE_DOMAIN"]
			}
		}
		out.Networks = append(out.Networks, schema.HostedNetworkView{Network: n, Available: e == nil, Domain: domain})
	}
	rows, e := db.From(ctx).Query(ctx, `SELECT id::text,network,username,handle,state,error FROM hosted_identity WHERE owner_id=$1 ORDER BY created_at`, social.Owner(ctx))
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var v schema.HostedIdentityView
		if e = rows.Scan(&v.ID, &v.Network, &v.Username, &v.Handle, &v.State, &v.Error); e != nil {
			return out, e
		}
		out.Identities = append(out.Identities, v)
	}
	return out, rows.Err()
}
func CreateHostedIdentity(ctx context.Context, req *router.Request[schema.HostedIdentityRequest]) (schema.HostedIdentityView, error) {
	var out schema.HostedIdentityView
	in := req.Body
	in.Username = strings.ToLower(strings.TrimSpace(in.Username))
	if in.Username == "zingmoderator" || !usernamePattern.MatchString(in.Username) || (in.Network == "bluesky" && strings.Contains(in.Username, "_")) {
		return out, router.Errorf(422, "Use lowercase letters and numbers; ActivityPub also supports underscores")
	}
	v, origin, e := configuration(in.Network)
	if e != nil {
		return out, e
	}
	owner := social.Owner(ctx)
	// Serialize per owner so retries reuse one persisted operation/password.
	tx, e := db.From(ctx).Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "hosted:"+owner+":"+in.Network); e != nil {
		return out, e
	}
	var userExists bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM auth_user WHERE subject=$1)`, owner).Scan(&userExists); e != nil {
		return out, e
	}
	if !userExists {
		return out, router.NotFound("User")
	}
	var sealed, server string
	e = tx.QueryRow(ctx, `SELECT id::text,network,username,handle,state,error,password_sealed,server FROM hosted_identity WHERE owner_id=$1 AND network=$2`, owner, in.Network).Scan(&out.ID, &out.Network, &out.Username, &out.Handle, &out.State, &out.Error, &sealed, &server)
	if errors.Is(e, pgx.ErrNoRows) {
		raw := make([]byte, 32)
		if _, e = rand.Read(raw); e != nil {
			return out, e
		}
		sealed, e = social.Seal(base64.RawURLEncoding.EncodeToString(raw))
		if e != nil {
			return out, e
		}
		e = tx.QueryRow(ctx, `INSERT INTO hosted_identity(owner_id,network,username,server,password_sealed) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING RETURNING id::text,network,username,handle,state,error`, owner, in.Network, in.Username, origin, sealed).Scan(&out.ID, &out.Network, &out.Username, &out.Handle, &out.State, &out.Error)
		if errors.Is(e, pgx.ErrNoRows) {
			return out, router.Errorf(409, "That hosted username is reserved")
		}
		server = origin
	}
	if e != nil {
		return out, e
	}
	if out.Username != in.Username && out.State == "rejected" {
		tag, err := tx.Exec(ctx, `UPDATE hosted_identity SET username=$3,state='pending',error='' WHERE owner_id=$1 AND id::text=$2 AND NOT EXISTS(SELECT 1 FROM hosted_identity WHERE network=$4 AND username=$3 AND id::text<>$2)`, owner, out.ID, in.Username, in.Network)
		if err != nil {
			return out, err
		}
		if tag.RowsAffected() != 1 {
			return out, router.Errorf(409, "That hosted username is reserved")
		}
		out.Username = in.Username
		out.State = "pending"
		out.Error = ""
	}
	if out.Username != in.Username {
		return out, router.Errorf(409, "This user already has a hosted identity or a pending creation on this network")
	}
	if server != origin {
		return out, router.Errorf(409, "The hosting origin changed; operator reconciliation is required")
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	if out.State == "active" {
		return out, nil
	}
	// A second transaction keeps the same lock during the remote call, while the
	// provisioning record above is already durable if this process stops.
	tx, e = db.From(ctx).Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "hosted:"+owner+":"+in.Network); e != nil {
		return out, e
	}
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM auth_user WHERE subject=$1)`, owner).Scan(&userExists); e != nil {
		return out, e
	}
	if !userExists {
		return out, router.NotFound("User")
	}
	if e = tx.QueryRow(ctx, `SELECT state,username,handle,password_sealed,server FROM hosted_identity WHERE id::text=$1 AND owner_id=$2`, out.ID, owner).Scan(&out.State, &out.Username, &out.Handle, &sealed, &server); e != nil {
		return out, e
	}
	if out.Username != in.Username || server != origin {
		return out, router.Errorf(409, "Hosted identity reservation changed; refresh before retrying")
	}
	if out.State == "active" {
		return out, nil
	}
	password, e := social.Open(sealed)
	if e != nil {
		return out, e
	}
	profile, e := auth.From(ctx).Profile(ctx, owner)
	if e != nil {
		return out, e
	}
	session, e := provision(ctx, v, origin, out, profile.Email, password)
	if e != nil {
		out.State = "failed"
		out.Error = "Hosted account creation is incomplete. Retry the same username; ambiguous backend outcomes require operator reconciliation."
		var rejected rejectedName
		if errors.As(e, &rejected) {
			out.State = "rejected"
			out.Error = rejected.Error()
		}
		if _, saveErr := tx.Exec(ctx, `UPDATE hosted_identity SET state=$3,error=$4 WHERE owner_id=$1 AND id::text=$2`, owner, out.ID, out.State, out.Error); saveErr != nil {
			return out, saveErr
		}
		if saveErr := tx.Commit(ctx); saveErr != nil {
			return out, saveErr
		}
		if out.State == "rejected" {
			return out, router.Errorf(422, "%s", out.Error)
		}
		return out, router.Errorf(502, "%s", out.Error)
	}
	a, e := social.Seal(session.Access)
	if e != nil {
		return out, e
	}
	r, e := social.Seal(session.Refresh)
	if e != nil {
		return out, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO social_connection(owner_id,network,server,subject,handle,access_sealed,refresh_sealed,expires_at,hosted) VALUES($1,$2,$3,$4,$5,$6,$7,$8,true) ON CONFLICT(owner_id,network,subject) DO UPDATE SET access_sealed=excluded.access_sealed,refresh_sealed=excluded.refresh_sealed,expires_at=excluded.expires_at,reconnect=false,hosted=true`, owner, session.Network, session.Server, session.Subject, session.Handle, a, r, session.ExpiresAt)
	if e != nil {
		return out, e
	}
	out.State = "active"
	out.Error = ""
	out.Handle = session.Handle
	_, e = tx.Exec(ctx, `UPDATE hosted_identity SET state='active',error='',subject=$3,handle=$4 WHERE owner_id=$1 AND id::text=$2`, owner, out.ID, session.Subject, session.Handle)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func provision(ctx context.Context, v map[string]string, origin string, id schema.HostedIdentityView, email, password string) (providers.Session, error) {
	s := providers.Session{Network: id.Network, Server: origin}
	var e error
	s.Client, e = providers.HostedClient(origin)
	if e != nil {
		return s, e
	}
	if id.Network == "mastodon" {
		agent := strings.TrimRight(v["ZING_ACTIVITYPUB_AGENT_URL"], "/")
		client, e := providers.HostedClient(agent)
		if e != nil {
			return s, e
		}
		body, marshalErr := json.Marshal(map[string]string{"id": id.ID, "username": id.Username, "email": id.ID + "@accounts.invalid", "password": password})
		if marshalErr != nil {
			return s, marshalErr
		}
		req, e := http.NewRequestWithContext(ctx, "POST", agent+"/provision", bytes.NewReader(body))
		if e != nil {
			return s, e
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+v["ZING_ACTIVITYPUB_AGENT_TOKEN"])
		res, e := client.Do(req)
		if e != nil {
			return s, e
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return s, errors.New("ActivityPub provisioning failed")
		}
		var result struct{ Subject, Handle, Access string }
		if e = json.NewDecoder(io.LimitReader(res.Body, 8192)).Decode(&result); e != nil {
			return s, e
		}
		if result.Subject == "" || result.Access == "" {
			return s, errors.New("incomplete hosted identity")
		}
		s.Subject = result.Subject
		s.Handle = result.Handle
		s.Access = result.Access
		return s, nil
	}
	s.Handle = id.Username + "." + strings.TrimPrefix(v["ZING_PDS_HANDLE_DOMAIN"], ".")
	// Resolve retries through credentials for this exact account before creating.
	var result struct {
		DID     string `json:"did"`
		Handle  string `json:"handle"`
		Access  string `json:"accessJwt"`
		Refresh string `json:"refreshJwt"`
	}
	e = s.Do(ctx, "POST", "/xrpc/com.atproto.server.createSession", map[string]string{"identifier": s.Handle, "password": password}, &result)
	if e != nil {
		var httpErr *router.HTTPError
		if !errors.As(e, &httpErr) || httpErr.Status != 409 {
			return s, e
		}
		req, e := http.NewRequestWithContext(ctx, "POST", origin+"/xrpc/com.atproto.server.createInviteCode", strings.NewReader(`{"useCount":1}`))
		if e != nil {
			return s, e
		}
		req.SetBasicAuth("admin", v["ZING_PDS_ADMIN_PASSWORD"])
		req.Header.Set("Content-Type", "application/json")
		res, e := s.Client.Do(req)
		if e != nil {
			return s, e
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return s, errors.New("PDS invite failed")
		}
		var invite struct {
			Code string `json:"code"`
		}
		if e = json.NewDecoder(io.LimitReader(res.Body, 8192)).Decode(&invite); e != nil {
			return s, e
		}
		body, err := json.Marshal(map[string]any{"email": email, "handle": s.Handle, "password": password, "inviteCode": invite.Code})
		if err != nil {
			return s, err
		}
		create, err := http.NewRequestWithContext(ctx, "POST", origin+"/xrpc/com.atproto.server.createAccount", bytes.NewReader(body))
		if err != nil {
			return s, err
		}
		create.Header.Set("Content-Type", "application/json")
		response, err := s.Client.Do(create)
		if err != nil {
			return s, errors.New("PDS account outcome is ambiguous")
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			var failure struct {
				Error string `json:"error"`
			}
			if err = json.NewDecoder(io.LimitReader(response.Body, 8192)).Decode(&failure); err != nil {
				return s, err
			}
			if response.StatusCode == 400 && (failure.Error == "HandleNotAvailable" || failure.Error == "InvalidHandle") {
				return s, rejectedName{}
			}
			return s, errors.New("PDS account creation could not be confirmed")
		}
		if err = json.NewDecoder(io.LimitReader(response.Body, 16384)).Decode(&result); err != nil {
			return s, err
		}
	}
	if result.DID == "" || result.Access == "" {
		return s, errors.New("PDS account response incomplete")
	}
	s.Subject = result.DID
	s.Handle = result.Handle
	s.Access = result.Access
	s.Refresh = result.Refresh
	t := time.Now().Add(90 * time.Minute)
	s.ExpiresAt = &t
	return s, nil
}
