// Package atview indexes genuine public records from the installation's PDS.
// The index is a read model; the PDS remains authoritative for identity and writes.
package atview

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/packs/realtime"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"net/url"
	"time"
	queries "zing/db/queries/gen"
	"zing/internal/providers"
)

func recordCollections() [6]string {
	return [6]string{"app.bsky.actor.profile", "app.bsky.feed.post", "app.bsky.graph.follow", "app.bsky.graph.block", "app.bsky.feed.like", "app.bsky.feed.repost"}
}

type sourceRecord struct {
	URI   string          `json:"uri"`
	CID   string          `json:"cid"`
	Value json.RawMessage `json:"value"`
}

func parseRecord(s providers.Session, collection, scan string, r sourceRecord) (queries.AtUpsertRecordParams, error) {
	var out queries.AtUpsertRecordParams
	uri, e := syntax.ParseATURI(r.URI)
	if e != nil || uri.Authority().String() != s.Subject || uri.Collection().String() != collection {
		return out, errors.New("PDS returned a record outside its requested repository")
	}
	if _, e = syntax.ParseCID(r.CID); e != nil {
		return out, errors.New("PDS returned an invalid record CID")
	}
	if len(r.Value) > 128*1024 {
		return out, errors.New("public record exceeds indexing limits")
	}
	var value struct {
		Type      string          `json:"$type"`
		Text      string          `json:"text"`
		CreatedAt string          `json:"createdAt"`
		Subject   json.RawMessage `json:"subject"`
		Reply     *struct {
			Parent providers.Ref `json:"parent"`
			Root   providers.Ref `json:"root"`
		} `json:"reply"`
	}
	if e = json.Unmarshal(r.Value, &value); e != nil {
		return out, errors.New("public record could not be decoded")
	}
	if value.Type != collection {
		return out, errors.New("public record type did not match its collection")
	}
	created := time.Now().UTC()
	subject := ""
	if collection == "app.bsky.actor.profile" {
		if uri.RecordKey().String() != "self" {
			return out, errors.New("invalid profile record key")
		}
	} else {
		if _, e = syntax.ParseTID(uri.RecordKey().String()); e != nil {
			return out, errors.New("invalid public record key")
		}
		created, e = time.Parse(time.RFC3339Nano, value.CreatedAt)
		if e != nil {
			return out, errors.New("public record timestamp is invalid")
		}
	}
	switch collection {
	case "app.bsky.graph.follow", "app.bsky.graph.block":
		if e = json.Unmarshal(value.Subject, &subject); e != nil {
			return out, errors.New("graph subject is invalid")
		}
		if _, e = syntax.ParseDID(subject); e != nil {
			return out, errors.New("graph subject is not a DID")
		}
	case "app.bsky.feed.like", "app.bsky.feed.repost":
		var ref providers.Ref
		if e = json.Unmarshal(value.Subject, &ref); e != nil {
			return out, errors.New("reaction subject is invalid")
		}
		if _, e = syntax.ParseATURI(ref.URI); e != nil {
			return out, e
		}
		if _, e = syntax.ParseCID(ref.CID); e != nil {
			return out, e
		}
		subject = ref.URI
	case "app.bsky.feed.post":
		if value.Reply != nil {
			if _, e = syntax.ParseATURI(value.Reply.Parent.URI); e != nil {
				return out, e
			}
			if _, e = syntax.ParseCID(value.Reply.Parent.CID); e != nil {
				return out, e
			}
			subject = value.Reply.Parent.URI
		}
	}
	return queries.AtUpsertRecordParams{Uri: r.URI, Did: s.Subject, Collection: collection, Cid: r.CID, Value: r.Value, Text: value.Text, Subject: subject, CreatedAt: created, ScanID: scan}, nil
}
func store(ctx context.Context, q *queries.Queries, tx pgx.Tx, r queries.AtUpsertRecordParams) error {
	prior, priorErr := q.AtPriorRecord(ctx, r.Uri)
	if priorErr != nil && !errors.Is(priorErr, pgx.ErrNoRows) {
		return priorErr
	}
	if e := q.AtUpsertRecord(ctx, r); e != nil {
		return e
	}
	if priorErr == nil && prior == r.Cid {
		return nil
	}
	if _, e := jobs.From(ctx).EnqueueTx(ctx, tx, "zing.at-public-update", nil, jobs.Unique("local-at-view")); e != nil {
		return e
	}
	if !errors.Is(priorErr, pgx.ErrNoRows) {
		return nil
	}
	kind, recipient, post := "", "", ""
	switch r.Collection {
	case "app.bsky.graph.follow":
		kind = "follow"
		recipient = r.Subject
	case "app.bsky.feed.like", "app.bsky.feed.repost", "app.bsky.feed.post":
		if r.Subject != "" {
			uri, e := syntax.ParseATURI(r.Subject)
			if e != nil {
				return e
			}
			recipient = uri.Authority().String()
			post = r.Subject
			if r.Collection == "app.bsky.feed.like" {
				kind = "like"
			} else if r.Collection == "app.bsky.feed.repost" {
				kind = "repost"
			} else {
				kind = "reply"
			}
		}
	}
	if kind != "" && recipient != "" && recipient != r.Did {
		return q.AtNotify(ctx, queries.AtNotifyParams{ID: kind + ":" + r.Uri, Recipient: recipient, Actor: r.Did, Kind: kind, RecordUri: r.Uri, PostUri: post, CreatedAt: r.CreatedAt})
	}
	return nil
}
func Refresh(ctx context.Context, s providers.Session) error {
	if !s.Hosted || s.Network != "bluesky" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	connection, e := db.From(ctx).Acquire(ctx)
	if e != nil {
		return e
	}
	defer connection.Release()
	lock := "at-index:" + s.Subject
	if e = queries.New(connection).AtSessionLock(ctx, lock); e != nil {
		return e
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := queries.New(connection).AtSessionUnlock(cleanup, lock); err != nil {
			connection.Conn().Close(cleanup)
		}
	}()
	exists, e := queries.New(connection).AtIdentityExists(ctx, s.Subject)
	if e != nil {
		return e
	}
	if !exists {
		return nil
	}
	for _, collection := range recordCollections() {
		id := s.Subject + ":" + collection
		state, err := queries.New(connection).AtSyncState(ctx, id)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if errors.Is(err, pgx.ErrNoRows) || state.Cursor == "" {
			state = queries.AtSyncStateRow{ID: id, Did: s.Subject, Collection: collection, ScanID: uuid.NewString(), StartedAt: time.Now().UTC(), CompletedAt: state.CompletedAt}
		}
		for page := 0; page < 8; page++ {
			var result struct {
				Records []sourceRecord `json:"records"`
				Cursor  string         `json:"cursor"`
			}
			query := url.Values{"repo": {s.Subject}, "collection": {collection}, "limit": {"100"}}
			if state.Cursor != "" {
				query.Set("cursor", state.Cursor)
			}
			if e = s.Do(ctx, "GET", "/xrpc/com.atproto.repo.listRecords?"+query.Encode(), nil, &result); e != nil {
				return e
			}
			if len(result.Records) > 100 || len(result.Cursor) > 8192 {
				return errors.New("PDS returned an invalid public-record page")
			}
			parsed := []queries.AtUpsertRecordParams{}
			for _, record := range result.Records {
				row, err := parseRecord(s, collection, state.ScanID, record)
				if err != nil {
					return err
				}
				parsed = append(parsed, row)
			}
			if result.Cursor != "" && result.Cursor == state.Cursor {
				return errors.New("PDS pagination did not advance")
			}
			tx, err := connection.Begin(ctx)
			if err != nil {
				return err
			}
			q := queries.New(tx)
			apply := func() error {
				for _, record := range parsed {
					if err := store(ctx, q, tx, record); err != nil {
						return err
					}
				}
				state.Cursor = result.Cursor
				state.Error = ""
				if state.Cursor == "" {
					now := time.Now().UTC()
					state.CompletedAt = &now
					if err := q.AtPruneNotifications(ctx, queries.AtPruneNotificationsParams{Did: s.Subject, Collection: collection, ScanID: state.ScanID, IndexedAt: state.StartedAt}); err != nil {
						return err
					}
					if err := q.AtPruneRecords(ctx, queries.AtPruneRecordsParams{Did: s.Subject, Collection: collection, ScanID: state.ScanID, IndexedAt: state.StartedAt}); err != nil {
						return err
					}
				}
				return q.AtStoreSyncState(ctx, queries.AtStoreSyncStateParams{ID: id, Did: s.Subject, Collection: collection, Cursor: state.Cursor, ScanID: state.ScanID, CompletedAt: state.CompletedAt, Error: state.Error, StartedAt: state.StartedAt})
			}
			if err = apply(); err != nil {
				return errors.Join(err, tx.Rollback(ctx))
			}
			if err = tx.Commit(ctx); err != nil {
				return err
			}
			if state.Cursor == "" {
				break
			}
		}
	}
	return nil
}

// RefreshOne verifies a confirmed publication at the PDS before indexing it.
// A concurrent paginated scan cannot prune this newer record.
func RefreshOne(ctx context.Context, s providers.Session, uri string) error {
	parsed, e := syntax.ParseATURI(uri)
	if e != nil || parsed.Authority().String() != s.Subject {
		return errors.New("cannot index a write for another account")
	}
	collection := parsed.Collection().String()
	known := false
	for _, c := range recordCollections() {
		known = known || c == collection
	}
	if !known {
		return errors.New("unsupported local record collection")
	}
	var record sourceRecord
	if e = s.Do(ctx, "GET", "/xrpc/com.atproto.repo.getRecord?"+url.Values{"repo": {s.Subject}, "collection": {collection}, "rkey": {parsed.RecordKey().String()}}.Encode(), nil, &record); e != nil {
		return e
	}
	row, e := parseRecord(s, collection, "direct:"+uuid.NewString(), record)
	if e != nil {
		return e
	}
	tx, e := db.From(ctx).Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	q := queries.New(tx)
	if e = q.AtSyncLock(ctx, "at-index:"+s.Subject); e != nil {
		return e
	}
	if exists, err := q.AtIdentityExists(ctx, s.Subject); err != nil {
		return err
	} else if !exists {
		return nil
	}
	if e = store(ctx, q, tx, row); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func RemoveAction(ctx context.Context, did, action, target string) error {
	collection := map[string]string{"unlike": "app.bsky.feed.like", "unrepost": "app.bsky.feed.repost", "unfollow": "app.bsky.graph.follow", "unblock": "app.bsky.graph.block"}[action]
	if collection == "" {
		return nil
	}
	tx, e := db.From(ctx).Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	q := queries.New(tx)
	if e = q.AtSyncLock(ctx, "at-index:"+did); e != nil {
		return e
	}
	if e = q.AtDeleteActionNotifications(ctx, queries.AtDeleteActionNotificationsParams{Did: did, Collection: collection, Subject: target}); e != nil {
		return e
	}
	if e = q.AtDeleteActionRecords(ctx, queries.AtDeleteActionRecordsParams{Did: did, Collection: collection, Subject: target}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func IsActionPresent(ctx context.Context, did, action, target string) (bool, error) {
	collection := map[string]string{"like": "app.bsky.feed.like", "repost": "app.bsky.feed.repost", "follow": "app.bsky.graph.follow", "block": "app.bsky.graph.block"}[action]
	if collection == "" {
		return false, nil
	}
	return queries.New(db.From(ctx)).AtOwnActionExists(ctx, queries.AtOwnActionExistsParams{Did: did, Collection: collection, Subject: target})
}
func Mute(ctx context.Context, owner, did, target string, on bool) error {
	if _, e := syntax.ParseDID(target); e != nil {
		return errors.New("invalid mute target")
	}
	q := queries.New(db.From(ctx))
	if on {
		return q.AtMute(ctx, queries.AtMuteParams{OwnerID: owner, Did: did, Target: target})
	}
	return q.AtUnmute(ctx, queries.AtUnmuteParams{OwnerID: owner, Did: did, Target: target})
}
func DeleteOwner(ctx context.Context, tx pgx.Tx, owner string) error {
	q := queries.New(tx)
	dids, e := q.AtOwnerIdentities(ctx, owner)
	if e != nil {
		return e
	}
	for _, did := range dids {
		if e = q.AtSyncLock(ctx, "at-index:"+did); e != nil {
			return e
		}
	}
	if e = q.AtRemoveOwnerNotifications(ctx, owner); e != nil {
		return e
	}
	if e = q.AtRemoveOwnerScans(ctx, owner); e != nil {
		return e
	}
	if e := q.AtRemoveActorRecords(ctx, owner); e != nil {
		return e
	}
	return q.AtRemoveOwnerPreferences(ctx, owner)
}

func Register(services *lidza.Services) {
	jobs.FromServices(services).Handle("zing.at-public-update", func(ctx context.Context, _ json.RawMessage) error {
		owners, e := queries.New(db.From(ctx)).AtActiveOwners(ctx)
		if e != nil {
			return e
		}
		for _, owner := range owners {
			if e = realtime.From(ctx).Publish(ctx, "user:"+owner, map[string]string{"kind": "hosted-feed-updated"}); e != nil {
				return e
			}
		}
		return nil
	})
}
