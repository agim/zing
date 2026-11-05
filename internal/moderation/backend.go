package moderation

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/env"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"net/url"
	"strings"
	queries "zing/db/queries/gen"
	"zing/internal/platform/vault"
	"zing/internal/providers"
)

// Backend calls use exact configured origins and never return upstream error
// bodies, which can contain credentials or private account data.
func backend(ctx context.Context, origin, method, path, bearer string, body any, admin bool, out any) error {
	c, e := providers.HostedClient(origin)
	if e != nil {
		return e
	}
	var raw io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		raw = bytes.NewReader(b)
	}
	r, e := http.NewRequestWithContext(ctx, method, origin+path, raw)
	if e != nil {
		return e
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	if admin {
		v, err := env.Values(".")
		if err != nil {
			return err
		}
		if v["ZING_PDS_ADMIN_PASSWORD"] == "" {
			return errors.New("pDS administration is not configured")
		}
		r.SetBasicAuth("admin", v["ZING_PDS_ADMIN_PASSWORD"])
	}
	res, e := c.Do(r)
	if e != nil {
		return errors.New("backend confirmation failed; retry is safe")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return errors.New("backend rejected moderation; inspect server health and permissions")
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
	}
	_, e = io.Copy(io.Discard, io.LimitReader(res.Body, 8192))
	return e
}
func agent(ctx context.Context, path string, body, out any) error {
	v, e := env.Values(".")
	if e != nil {
		return e
	}
	origin := strings.TrimRight(v["ZING_ACTIVITYPUB_AGENT_URL"], "/")
	if origin == "" || v["ZING_ACTIVITYPUB_AGENT_TOKEN"] == "" {
		return errors.New("private ActivityPub administration agent is not configured")
	}
	return backend(ctx, origin, "POST", path, v["ZING_ACTIVITYPUB_AGENT_TOKEN"], body, false, out)
}

// ActivityPubAdmin creates one protected service identity with independently
// generated credentials. Persist before provisioning so retries reuse it.
func ActivityPubAdmin(ctx context.Context, origin string) (string, error) {
	tx, e := db.From(ctx).Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	q := queries.New(tx)
	if e = q.ChatLock(ctx, "moderation-backend:"+origin); e != nil {
		return "", e
	}
	row, e := q.ModerationBackendCredential(ctx, origin)
	if errors.Is(e, pgx.ErrNoRows) {
		raw := make([]byte, 32)
		if _, e = rand.Read(raw); e != nil {
			return "", e
		}
		sealed, err := vault.Seal(base64.RawURLEncoding.EncodeToString(raw))
		if err != nil {
			return "", err
		}
		row, e = q.ModerationCreateBackendCredential(ctx, queries.ModerationCreateBackendCredentialParams{ID: uuid.NewString(), Server: origin, PasswordSealed: sealed})
	}
	if e != nil {
		return "", e
	}
	if e = tx.Commit(ctx); e != nil {
		return "", e
	}
	if row.AccessSealed != "" {
		return vault.Open(row.AccessSealed)
	}
	// Reprovisioning issues a fresh token and also confirms the native admin role.
	password, e := vault.Open(row.PasswordSealed)
	if e != nil {
		return "", e
	}
	u, e := url.Parse(origin)
	if e != nil {
		return "", e
	}
	var result struct {
		AccessToken string `json:"access"`
	}
	e = agent(ctx, "/provision", map[string]any{"id": row.ID, "username": "zingmoderator", "email": "zingmoderator@" + u.Hostname(), "password": password, "administrator": true}, &result)
	if e != nil {
		return "", e
	}
	if result.AccessToken == "" {
		return "", errors.New("missing native moderation token")
	}
	sealed, e := vault.Seal(result.AccessToken)
	if e != nil {
		return "", e
	}
	if e = queries.New(db.From(ctx)).ModerationStoreBackendAccess(ctx, queries.ModerationStoreBackendAccessParams{ID: row.ID, AccessSealed: sealed}); e != nil {
		return "", e
	}
	return result.AccessToken, nil
}
