package hosting

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	queries "zing/db/queries/gen"
	"zing/internal/moderation"
	"zing/internal/providers"
	"zing/internal/social"
)

type deletion struct{ Network, Server, Subject, Username, OperationID, PasswordSealed, AccessSealed string }

// QueueDeletion copies only sealed credentials into durable jobs before the
// auth transaction removes local rows. Failures remain visible to operators.
func QueueDeletion(ctx context.Context, tx pgx.Tx, owner string) error {
	if e := queries.New(tx).EnrollmentDelete(ctx, owner); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, `DELETE FROM job WHERE kind IN ('zing.enrollment','zing.provision-identity') AND (payload->>'ownerId'=$1 OR payload#>>'{}'=$1) AND state IN ('pending','failed')`, owner); e != nil {
		return e
	}
	for _, network := range []string{"mastodon", "bluesky"} {
		if _, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "hosted:"+owner+":"+network); e != nil {
			return e
		}
	}

	rows, e := tx.Query(ctx, `SELECT h.network,h.server,h.subject,h.username,h.id::text,h.password_sealed,coalesce(c.access_sealed,'') FROM hosted_identity h LEFT JOIN social_connection c ON c.owner_id=h.owner_id AND c.network=h.network AND c.subject=h.subject AND c.hosted WHERE h.owner_id=$1 AND h.state<>'rejected'`, owner)
	if e != nil {
		return e
	}
	identities := []deletion{}
	for rows.Next() {
		var d deletion
		if e = rows.Scan(&d.Network, &d.Server, &d.Subject, &d.Username, &d.OperationID, &d.PasswordSealed, &d.AccessSealed); e != nil {
			rows.Close()
			return e
		}
		identities = append(identities, d)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, d := range identities {
		if d.Subject == "" {
			return router.Errorf(409, "Resolve incomplete hosted identity provisioning before deleting this account")
		}
		if _, e = jobs.From(ctx).EnqueueTx(ctx, tx, "zing.delete-hosted", d); e != nil {
			return e
		}
	}
	_, e = tx.Exec(ctx, `DELETE FROM hosted_identity WHERE owner_id=$1`, owner)
	return e
}
func Register(services *lidza.Services) {
	jobs.FromServices(services).Handle("zing.delete-hosted", func(ctx context.Context, raw json.RawMessage) error {
		var d deletion
		if e := json.Unmarshal(raw, &d); e != nil {
			return e
		}
		client, e := providers.HostedClient(d.Server)
		if e != nil {
			return e
		}
		var req *http.Request
		if d.Network == "mastodon" {
			password, e := social.Open(d.PasswordSealed)
			if e != nil {
				return e
			}
			access, e := social.Open(d.AccessSealed)
			if e != nil {
				return e
			}
			req, e = http.NewRequestWithContext(ctx, "POST", d.Server+"/api/v1/accounts/delete", strings.NewReader(url.Values{"password": {password}}.Encode()))
			if e != nil {
				return e
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Authorization", "Bearer "+access)
		} else if d.Network == "bluesky" {
			values, e := env.Values(".")
			if e != nil {
				return e
			}
			body, e := json.Marshal(map[string]string{"did": d.Subject})
			if e != nil {
				return e
			}
			req, e = http.NewRequestWithContext(ctx, "POST", d.Server+"/xrpc/com.atproto.admin.deleteAccount", strings.NewReader(string(body)))
			if e != nil {
				return e
			}
			req.SetBasicAuth("admin", values["ZING_PDS_ADMIN_PASSWORD"])
			req.Header.Set("Content-Type", "application/json")
		} else {
			return errors.New("unknown hosted network")
		}
		res, e := client.Do(req)
		if e != nil {
			return errors.New("hosted deletion could not be confirmed")
		}
		defer res.Body.Close()
		if d.Network == "mastodon" {
			res.Body.Close()
			return moderation.ConfirmActivityPubErasure(ctx, d.Server, d.Subject, d.Username, d.OperationID, res.StatusCode >= 200 && res.StatusCode < 300)
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return errors.New("hosted deletion needs retry or operator reconciliation")
		}
		_, e = io.Copy(io.Discard, io.LimitReader(res.Body, 8192))
		return e
	})
}
