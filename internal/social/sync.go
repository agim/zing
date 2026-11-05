package social

import (
	"context"
	"encoding/json"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/cache"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/packs/realtime"
	"time"
	"zing/internal/atview"
)

type syncPayload struct{ Owner, Connection string }

func RegisterSync(s *lidza.Services) error {
	q := jobs.FromServices(s)
	q.Handle("zing.sync-dispatch", func(ctx context.Context, _ json.RawMessage) error {
		tx, e := db.From(ctx).Begin(ctx)
		if e != nil {
			return e
		}
		defer tx.Rollback(ctx)
		rows, e := tx.Query(ctx, `SELECT owner_id,id::text FROM social_connection WHERE NOT reconnect AND (synced_at IS NULL OR synced_at<now()-interval '2 minutes') ORDER BY synced_at NULLS FIRST LIMIT 50 FOR UPDATE SKIP LOCKED`)
		if e != nil {
			return e
		}
		items := []syncPayload{}
		for rows.Next() {
			var p syncPayload
			if e = rows.Scan(&p.Owner, &p.Connection); e != nil {
				rows.Close()
				return e
			}
			items = append(items, p)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, p := range items {
			if _, e = q.EnqueueTx(ctx, tx, "zing.sync", p, jobs.Unique(p.Connection)); e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `UPDATE social_connection SET synced_at=now() WHERE owner_id=$1 AND id::text=$2`, p.Owner, p.Connection); e != nil {
				return e
			}
		}
		// Only expired OAuth request state is removed; live sessions remain sealed.
		if _, e = tx.Exec(ctx, `DELETE FROM o_auth_attempt WHERE expires_at<now()`); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `DELETE FROM o_auth_data WHERE expires_at<now()`); e != nil {
			return e
		}
		return tx.Commit(ctx)
	})
	q.Handle("zing.sync", func(ctx context.Context, raw json.RawMessage) error {
		var p syncPayload
		if e := json.Unmarshal(raw, &p); e != nil {
			return e
		}
		session, e := Load(ctx, p.Owner, p.Connection)
		if e != nil {
			return nil
		}
		if session.LocalView {
			if e = atview.Refresh(ctx, session); e != nil {
				return e
			}
		}
		feed, e := session.Feed(ctx, "", "")
		if e != nil {
			return e
		}
		if e = cache.From(ctx).Set(ctx, "feed:"+p.Owner+":"+p.Connection, feed, 45*time.Second); e != nil {
			return e
		}
		return realtime.From(ctx).Publish(ctx, "user:"+p.Owner, map[string]string{"kind": "feed-updated", "connectionId": p.Connection})
	})
	return q.Schedule("zing.sync-dispatch", jobs.Every(30*time.Second), nil)
}
