package publishing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/packs/storage"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	"zing/internal/providers"
	"zing/internal/social"
	"zing/schema"
)

type payload struct{ Owner, Delivery string }

func Register(s *lidza.Services) { jobs.FromServices(s).Handle("zing.publish", run) }
func Validate(ctx context.Context, owner string, in schema.ComposeInput) error {
	if strings.TrimSpace(in.Text) == "" {
		return router.Errorf(422, "Write something first")
	}
	if _, e := time.LoadLocation(in.Timezone); e != nil {
		return router.Errorf(422, "Choose a valid timezone")
	}
	seen := map[string]bool{}
	for _, id := range in.Destinations {
		if seen[id] {
			return router.Errorf(422, "Duplicate destination")
		}
		seen[id] = true
		s, e := social.Load(ctx, owner, id)
		if e != nil {
			return e
		}
		limit := 500
		if s.Network == "bluesky" {
			limit = 300
		}
		if utf8.RuneCountInString(in.Text) > limit {
			return router.Errorf(422, "Text exceeds the %s limit of %d characters", s.Network, limit)
		}

		if in.RelayParentDraftID != "" {
			if _, err := resolveReply(ctx, owner, id, s, in); err != nil {
				return err
			}
		}
		if in.RelayParentDraftID == "" && in.ReplyID != "" && id != in.ReplyConnectionID {
			return router.Errorf(422, "Replies require the source connection. Use explicit mapped relaying for another destination")
		}
		if in.RelayParentDraftID == "" && in.ReplyID != "" && s.Network == "bluesky" && (in.ReplyCid == "" || in.RootID == "" || in.RootCid == "") {
			return router.Errorf(422, "Bluesky replies require parent and root references")
		}
	}
	for _, id := range in.MediaIDs {
		var exists bool
		if e := db.From(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM media WHERE owner_id=$1 AND id::text=$2)`, owner, id).Scan(&exists); e != nil {
			return e
		}
		if !exists {
			return router.NotFound("Media")
		}
	}
	return nil
}
func SaveDraft(ctx context.Context, req *router.Request[schema.ComposeRequest]) (schema.DraftView, error) {
	out := schema.DraftView{}
	var in schema.ComposeInput
	if err := social.Convert(req.Body, &in); err != nil {
		return out, err
	}
	if in.Destinations == nil {
		in.Destinations = []string{}
	}
	if in.MediaIDs == nil {
		in.MediaIDs = []string{}
	}
	if in.Timezone == "" {
		in.Timezone = "UTC"
	}
	owner := social.Owner(ctx)
	if e := Validate(ctx, owner, in); e != nil {
		return out, e
	}
	var when *time.Time
	if in.ScheduledAt != "" {
		t, e := time.Parse(time.RFC3339, in.ScheduledAt)
		if e != nil || !t.After(time.Now()) {
			return out, router.Errorf(422, "Schedule a time in the future")
		}
		when = &t
	}
	destinations, e := json.Marshal(in.Destinations)
	if e != nil {
		return out, e
	}
	media, e := json.Marshal(in.MediaIDs)
	if e != nil {
		return out, e
	}
	reply, e := json.Marshal(in)
	if e != nil {
		return out, e
	}
	tx, e := db.From(ctx).Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	id := req.Param("id")
	if id == "" {
		e = tx.QueryRow(ctx, `INSERT INTO draft(owner_id,text,destinations,media_ids,reply_to,scheduled_at,timezone) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id::text`, owner, in.Text, destinations, media, reply, when, in.Timezone).Scan(&id)
	} else {
		tag, err := tx.Exec(ctx, `UPDATE draft SET text=$3,destinations=$4,media_ids=$5,reply_to=$6,scheduled_at=$7,timezone=$8,updated_at=now() WHERE owner_id=$1 AND id::text=$2 AND status='draft'`, owner, id, in.Text, destinations, media, reply, when, in.Timezone)
		e = err
		if e == nil && tag.RowsAffected() == 0 {
			e = router.NotFound("Editable draft")
		}
	}
	if e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	var composition schema.ComposeRequest
	if e = social.Convert(in, &composition); e != nil {
		return out, e
	}
	return schema.DraftView{Composition: &composition, ID: id, Text: in.Text, Destinations: in.Destinations, MediaIDs: in.MediaIDs, Status: "draft", ScheduledAt: in.ScheduledAt, Timezone: in.Timezone}, nil
}
func Drafts(ctx context.Context, req *router.Request[router.None]) (schema.DraftList, error) {
	out := schema.DraftList{Items: []schema.DraftView{}}
	rows, e := db.From(ctx).Query(ctx, `SELECT id::text,text,destinations,media_ids,status,scheduled_at,timezone,reply_to FROM draft WHERE owner_id=$1 ORDER BY updated_at DESC LIMIT 100`, social.Owner(ctx))
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var v schema.DraftView
		var d, m, composition []byte
		var t *time.Time
		if e = rows.Scan(&v.ID, &v.Text, &d, &m, &v.Status, &t, &v.Timezone, &composition); e != nil {
			return out, e
		}
		if e = json.Unmarshal(d, &v.Destinations); e != nil {
			return out, e
		}
		if e = json.Unmarshal(m, &v.MediaIDs); e != nil {
			return out, e
		}
		if t != nil {
			v.ScheduledAt = t.Format(time.RFC3339)
		}
		if len(composition) > 0 && string(composition) != "null" {
			var c schema.ComposeRequest
			if e = json.Unmarshal(composition, &c); e != nil {
				return out, e
			}
			v.Composition = &c
		}
		out.Items = append(out.Items, v)
	}
	return out, rows.Err()
}
func Publish(ctx context.Context, req *router.Request[router.None]) (schema.OperationResult, error) {
	owner, id := social.Owner(ctx), req.Param("id")
	tx, e := db.From(ctx).Begin(ctx)
	if e != nil {
		return schema.OperationResult{}, e
	}
	defer tx.Rollback(ctx)
	var input []byte
	var state string
	var when *time.Time
	e = tx.QueryRow(ctx, `SELECT reply_to,status,scheduled_at FROM draft WHERE owner_id=$1 AND id::text=$2 FOR UPDATE`, owner, id).Scan(&input, &state, &when)
	if errors.Is(e, pgx.ErrNoRows) {
		return schema.OperationResult{}, router.NotFound("Draft")
	}
	if e != nil {
		return schema.OperationResult{}, e
	}
	if state != "draft" {
		return schema.OperationResult{Ok: true}, nil
	}
	var in schema.ComposeInput
	if e = json.Unmarshal(input, &in); e != nil {
		return schema.OperationResult{}, e
	}
	if len(in.Destinations) == 0 {
		return schema.OperationResult{}, router.Errorf(422, "Choose at least one connected account before publishing")
	}
	if e = Validate(ctx, owner, in); e != nil {
		return schema.OperationResult{}, e
	}
	for _, connection := range in.Destinations {
		var delivery string
		e = tx.QueryRow(ctx, `INSERT INTO delivery(owner_id,draft_id,connection_id) VALUES($1,$2,$3) ON CONFLICT(draft_id,connection_id) DO UPDATE SET draft_id=excluded.draft_id RETURNING id::text`, owner, id, connection).Scan(&delivery)
		if e != nil {
			return schema.OperationResult{}, e
		}
		opts := []jobs.Option{jobs.Unique(delivery)}
		if when != nil {
			opts = append(opts, jobs.RunAt(*when))
		}
		if _, e = jobs.From(ctx).EnqueueTx(ctx, tx, "zing.publish", payload{Owner: owner, Delivery: delivery}, opts...); e != nil {
			return schema.OperationResult{}, e
		}
	}
	_, e = tx.Exec(ctx, `UPDATE draft SET status='queued',updated_at=now() WHERE owner_id=$1 AND id::text=$2`, owner, id)
	if e != nil {
		return schema.OperationResult{}, e
	}
	e = tx.Commit(ctx)
	return schema.OperationResult{Ok: e == nil}, e
}
func Cancel(ctx context.Context, req *router.Request[router.None]) (schema.OperationResult, error) {
	owner, id := social.Owner(ctx), req.Param("id")
	tx, e := db.From(ctx).Begin(ctx)
	if e != nil {
		return schema.OperationResult{}, e
	}
	defer tx.Rollback(ctx)
	var status string
	e = tx.QueryRow(ctx, `SELECT status FROM draft WHERE owner_id=$1 AND id::text=$2 FOR UPDATE`, owner, id).Scan(&status)
	if errors.Is(e, pgx.ErrNoRows) {
		return schema.OperationResult{}, router.NotFound("Draft")
	}
	if e != nil {
		return schema.OperationResult{}, e
	}
	_, e = tx.Exec(ctx, `UPDATE draft SET status='cancelled',updated_at=now() WHERE owner_id=$1 AND id::text=$2`, owner, id)
	if e != nil {
		return schema.OperationResult{}, e
	}
	_, e = tx.Exec(ctx, `UPDATE delivery SET state='cancelled',updated_at=now() WHERE owner_id=$1 AND draft_id::text=$2 AND state IN ('pending','failed')`, owner, id)
	if e != nil {
		return schema.OperationResult{}, e
	}
	e = tx.Commit(ctx)
	return schema.OperationResult{Ok: e == nil}, e
}
func Deliveries(ctx context.Context, req *router.Request[router.None]) (schema.DeliveryList, error) {
	out := schema.DeliveryList{Items: []schema.DeliveryView{}}
	rows, e := db.From(ctx).Query(ctx, `SELECT id::text,draft_id::text,connection_id::text,state,remote_url,error,attempts FROM delivery WHERE owner_id=$1 ORDER BY updated_at DESC LIMIT 100`, social.Owner(ctx))
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var d schema.DeliveryView
		if e = rows.Scan(&d.ID, &d.DraftID, &d.ConnectionID, &d.State, &d.RemoteURL, &d.Error, &d.Attempts); e != nil {
			return out, e
		}
		out.Items = append(out.Items, d)
	}
	return out, rows.Err()
}
func Retry(ctx context.Context, req *router.Request[router.None]) (schema.OperationResult, error) {
	owner, id := social.Owner(ctx), req.Param("id")
	tx, e := db.From(ctx).Begin(ctx)
	if e != nil {
		return schema.OperationResult{}, e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, `UPDATE delivery SET state='pending',error='',updated_at=now() WHERE owner_id=$1 AND id::text=$2 AND state='failed' AND EXISTS(SELECT 1 FROM draft WHERE owner_id=$1 AND draft.id=delivery.draft_id AND status<>'cancelled')`, owner, id)
	if e != nil {
		return schema.OperationResult{}, e
	}
	if tag.RowsAffected() == 0 {
		return schema.OperationResult{}, router.Errorf(409, "Only confirmed failed deliveries can be retried. Reconcile uncertain deliveries first.")
	}
	if _, e = jobs.From(ctx).EnqueueTx(ctx, tx, "zing.publish", payload{Owner: owner, Delivery: id}); e != nil {
		return schema.OperationResult{}, e
	}
	e = tx.Commit(ctx)
	return schema.OperationResult{Ok: e == nil}, e
}
func run(ctx context.Context, raw json.RawMessage) error {
	var p payload
	if e := json.Unmarshal(raw, &p); e != nil {
		return e
	}
	// A session-level owner lock serializes deletion with network side effects.
	conn, e := db.From(ctx).Acquire(ctx)
	if e != nil {
		return e
	}
	defer conn.Release()
	var locked bool
	if e = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, p.Owner).Scan(&locked); e != nil {
		return e
	}
	if !locked {
		return errors.New("another publication is active for this account")
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, p.Owner); err != nil {
			conn.Conn().Close(unlockCtx)
		}
	}()
	tx, e := conn.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var state, draftID, connection, text, draftState string
	var media, reply []byte
	e = tx.QueryRow(ctx, `SELECT d.state,d.draft_id::text,d.connection_id::text,p.text,p.media_ids,p.reply_to,p.status FROM delivery d JOIN draft p ON p.id=d.draft_id AND p.owner_id=d.owner_id WHERE d.owner_id=$1 AND d.id::text=$2 FOR UPDATE OF d,p`, p.Owner, p.Delivery).Scan(&state, &draftID, &connection, &text, &media, &reply, &draftState)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if state == "published" || state == "cancelled" || state == "uncertain" || draftState == "cancelled" {
		return nil
	}
	if state == "sending" {
		_, e = tx.Exec(ctx, `UPDATE delivery SET state='uncertain',error='Worker restarted during delivery. Reconcile before retrying.',updated_at=now() WHERE owner_id=$1 AND id::text=$2`, p.Owner, p.Delivery)
		if e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	s, e := social.Load(ctx, p.Owner, connection)
	if e != nil {
		_, err := tx.Exec(ctx, `UPDATE delivery SET state='failed',error='Reconnect the destination account',updated_at=now() WHERE owner_id=$1 AND id::text=$2`, p.Owner, p.Delivery)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	_, e = tx.Exec(ctx, `UPDATE delivery SET state='sending',attempts=attempts+1,updated_at=now() WHERE owner_id=$1 AND id::text=$2`, p.Owner, p.Delivery)
	if e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	var ids []string
	if e = json.Unmarshal(media, &ids); e != nil {
		return e
	}
	images := []providers.Image{}
	for _, id := range ids {
		var key, content, alt string
		if e = db.From(ctx).QueryRow(ctx, `SELECT key,content_type,alt FROM media WHERE owner_id=$1 AND id::text=$2`, p.Owner, id).Scan(&key, &content, &alt); e != nil {
			return finish(ctx, p, "failed", "Media unavailable", providers.Ref{}, "")
		}
		r, _, err := storage.From(ctx).Get(ctx, key)
		if err != nil {
			return finish(ctx, p, "failed", "Media unavailable", providers.Ref{}, "")
		}
		data, err := io.ReadAll(io.LimitReader(r, 8<<20))
		closeErr := r.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return finish(ctx, p, "failed", "Media could not be read", providers.Ref{}, "")
		}
		sourceURL := ""
		if s.Network == "threads" {
			sourceURL, e = storage.From(ctx).PresignGet(ctx, key, time.Hour)
			if e != nil {
				return finish(ctx, p, "failed", "Threads images require configured HTTPS object storage", providers.Ref{}, "")
			}
		}
		images = append(images, providers.Image{Data: data, ContentType: content, Alt: alt, SourceURL: sourceURL})
	}
	var in schema.ComposeInput
	if e = json.Unmarshal(reply, &in); e != nil {
		return e
	}
	in, e = resolveReply(ctx, p.Owner, connection, s, in)
	if e != nil {
		return finish(ctx, p, "failed", "Relay parent is unavailable", providers.Ref{}, "")
	}
	ref, remote, e := s.Publish(ctx, text, p.Delivery, &in, images)
	if e != nil {
		status, message := "uncertain", "Network outcome could not be confirmed. Check the destination before retrying."
		var h *router.HTTPError
		if errors.As(e, &h) && h.Status < 500 {
			status = "failed"
			message = h.Message
		}
		if err := finish(ctx, p, status, message, providers.Ref{}, ""); err != nil {
			return err
		}
		if status == "failed" && h != nil && h.Status == 429 {
			return fmt.Errorf("network rate limited")
		}
		return nil
	}
	return finish(ctx, p, "published", "", ref, remote)
}
func finish(ctx context.Context, p payload, state, message string, ref providers.Ref, remote string) error {
	_, e := db.From(ctx).Exec(ctx, `UPDATE delivery SET state=$3,error=$4,remote_id=$5,remote_cid=$6,remote_url=$7,updated_at=now() WHERE owner_id=$1 AND id::text=$2`, p.Owner, p.Delivery, state, message, ref.URI, ref.CID, remote)
	return e
}

func UpdateDraft(ctx context.Context, req *router.Request[schema.ComposeRequest]) (schema.DraftView, error) {
	return SaveDraft(ctx, req)
}

// Reconcile verifies a suspected publication at its source before recording
// success. A user's claim alone never makes an uncertain delivery retryable.
func Reconcile(ctx context.Context, req *router.Request[schema.ReconcileRequest]) (schema.OperationResult, error) {
	owner, id := social.Owner(ctx), req.Param("id")
	var connection, text, state string
	e := db.From(ctx).QueryRow(ctx, `SELECT d.connection_id::text,p.text,d.state FROM delivery d JOIN draft p ON p.id=d.draft_id AND p.owner_id=d.owner_id WHERE d.owner_id=$1 AND d.id::text=$2`, owner, id).Scan(&connection, &text, &state)
	if errors.Is(e, pgx.ErrNoRows) {
		return schema.OperationResult{}, router.NotFound("Delivery")
	}
	if e != nil {
		return schema.OperationResult{}, e
	}
	if state != "uncertain" {
		return schema.OperationResult{}, router.Errorf(409, "Only uncertain deliveries need reconciliation")
	}
	s, e := social.Load(ctx, owner, connection)
	if e != nil {
		return schema.OperationResult{}, e
	}
	post, e := s.Post(ctx, req.Body.RemoteID)
	if e != nil {
		return schema.OperationResult{}, e
	}
	if post.Author.ID != s.Subject || strings.TrimSpace(post.Text) != strings.TrimSpace(text) {
		return schema.OperationResult{}, router.Errorf(422, "The post must match this account and the original text")
	}
	e = finish(ctx, payload{Owner: owner, Delivery: id}, "published", "", providers.Ref{URI: post.ID, CID: post.Cid}, post.URL)
	return schema.OperationResult{Ok: e == nil}, e
}
