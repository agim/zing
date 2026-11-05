package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/pkg/env"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"net/url"
	"strings"
	"time"
	queries "zing/db/queries/gen"
	"zing/internal/platform/vault"
)

func Register(s *lidza.Services) { jobs.FromServices(s).Handle("zing.moderate", run) }
func run(ctx context.Context, raw json.RawMessage) error {
	var id string
	if e := json.Unmarshal(raw, &id); e != nil {
		return e
	}
	conn, e := db.From(ctx).Acquire(ctx)
	if e != nil {
		return e
	}
	defer conn.Release()
	q := queries.New(conn)
	a, e := q.ModerationAction(ctx, id)
	if e != nil {
		return e
	}
	lock := "moderation-account:" + a.Target
	if e = q.AtSessionLock(ctx, lock); e != nil {
		return e
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := q.AtSessionUnlock(cleanup, lock); err != nil {
			conn.Conn().Close(cleanup)
		}
	}()
	a, e = q.ModerationAction(ctx, id)
	if e != nil {
		return e
	}
	if a.State == "applied" || a.State == "cancelled" || a.State == "superseded" {
		return nil
	}
	if a.Network == "all" {
		exists, err := q.ModerationOwnerExists(ctx, a.Target)
		if err != nil {
			return err
		}
		newer, err := q.ModerationNewerAction(ctx, id)
		if err != nil {
			return err
		}
		if !exists || newer {
			return q.ModerationSetActionState(ctx, queries.ModerationSetActionStateParams{ID: id, State: "superseded"})
		}
	}
	e = apply(ctx, q, a)
	if e != nil {
		if err := q.ModerationSetActionState(ctx, queries.ModerationSetActionStateParams{ID: id, State: "failed", Error: "Native server confirmation failed. The worker will retry; inspect server health and permissions."}); err != nil {
			return err
		}
		return e
	}
	if a.Kind == "restore" {
		if e = q.ModerationSetSuspended(ctx, queries.ModerationSetSuspendedParams{OwnerID: a.Target, Suspended: false, Reason: a.Reason}); e != nil {
			return e
		}
		if e = q.ModerationRefreshConnection(ctx, a.Target); e != nil {
			return e
		}
	}
	return q.ModerationSetActionState(ctx, queries.ModerationSetActionStateParams{ID: id, State: "applied"})
}
func apply(ctx context.Context, q *queries.Queries, a queries.ModerationAction) error {
	if a.Network == "all" {
		identities, e := q.ModerationHostedAccounts(ctx, a.Target)
		if e != nil {
			return e
		}
		for _, h := range identities {
			if e = q.ModerationAddDestination(ctx, queries.ModerationAddDestinationParams{ActionID: a.ID, Network: h.Network}); e != nil {
				return e
			}
		}
		destinations, e := q.ModerationDestinations(ctx, a.ID)
		if e != nil {
			return e
		}
		for _, d := range destinations {
			if d.State == "applied" {
				continue
			}
			found := false
			for _, h := range identities {
				if h.Network != d.Network {
					continue
				}
				found = true
				if h.Network == "mastodon" {
					verb := "disable"
					if a.Kind == "restore" {
						verb = "enable"
					}
					e = agent(ctx, "/account-state", map[string]string{"action": verb, "username": h.Username}, nil)
				} else if h.Network == "bluesky" {
					e = backend(ctx, h.Server, "POST", "/xrpc/com.atproto.admin.updateSubjectStatus", "", map[string]any{"subject": map[string]string{"$type": "com.atproto.admin.defs#repoRef", "did": h.Subject}, "takedown": map[string]bool{"applied": a.Kind == "suspend"}}, true, nil)
				} else {
					e = errors.New("unknown hosted network")
				}
				state, note := "applied", ""
				if e != nil {
					state = "failed"
					note = "Native server did not confirm this destination"
				}
				if err := q.ModerationDestinationState(ctx, queries.ModerationDestinationStateParams{ActionID: a.ID, Network: h.Network, State: state, Error: note}); err != nil {
					return err
				}
				if e != nil {
					return e
				}
			}
			if !found {
				return errors.New("hosted identity is no longer active")
			}
		}
		return nil
	}
	values, e := env.Values(".")
	if e != nil {
		return e
	}
	origin := strings.TrimRight(values["ZING_ACTIVITYPUB_URL"], "/")
	if a.Kind == "remove_post" {
		owner, e := q.ModerationPostOwner(ctx, queries.ModerationPostOwnerParams{Network: a.Network, Target: a.Target})
		if e != nil {
			return e
		}
		identities, e := q.ModerationHostedAccounts(ctx, owner)
		if e != nil {
			return e
		}
		for _, h := range identities {
			if h.Network != a.Network {
				continue
			}
			if a.Network == "bluesky" {
				uri, e := syntax.ParseATURI(a.Target)
				if e != nil || uri.Authority().String() != h.Subject || uri.Collection().String() != "app.bsky.feed.post" {
					return errors.New("reported post identity does not match")
				}
				var record struct{ URI, CID string }
				if e = backend(ctx, h.Server, "GET", "/xrpc/com.atproto.repo.getRecord?"+url.Values{"repo": {h.Subject}, "collection": {"app.bsky.feed.post"}, "rkey": {uri.RecordKey().String()}}.Encode(), "", nil, true, &record); e != nil {
					return e
				}
				return backend(ctx, h.Server, "POST", "/xrpc/com.atproto.admin.updateSubjectStatus", "", map[string]any{"subject": map[string]string{"$type": "com.atproto.repo.strongRef", "uri": a.Target, "cid": record.CID}, "takedown": map[string]bool{"applied": true}}, true, nil)
			}
			// The native admin token verifies the status belongs to the reported local
			// identity before using GoToSocial's administrative status removal API.
			token, e := ActivityPubAdmin(ctx, h.Server)
			if e != nil {
				return e
			}
			var post struct{ Account struct{ ID string } }
			if e = backend(ctx, h.Server, "GET", "/api/v1/statuses/"+url.PathEscape(a.Target), token, nil, false, &post); e != nil {
				return e
			}
			if post.Account.ID != h.Subject {
				return errors.New("reported post identity does not match")
			}
			// Native user credentials authorize deletion; no access to private inboxes.
			connection, e := q.ModerationHostedSession(ctx, queries.ModerationHostedSessionParams{Network: h.Network, Server: h.Server, Subject: h.Subject})
			if e != nil {
				return e
			}
			access, e := vault.Open(connection.AccessSealed)
			if e != nil {
				return e
			}
			return backend(ctx, h.Server, "DELETE", "/api/v1/statuses/"+url.PathEscape(a.Target), access, nil, false, nil)
		}
		return errors.New("reported identity is no longer active")
	}
	token, e := ActivityPubAdmin(ctx, origin)
	if e != nil {
		return e
	}
	var blocks []struct{ ID, Domain string }
	if e = backend(ctx, origin, "GET", "/api/v1/admin/domain_blocks", token, nil, false, &blocks); e != nil {
		return e
	}
	for _, b := range blocks {
		if b.Domain == a.Target {
			if a.Kind == "block_domain" {
				return nil
			}
			return backend(ctx, origin, "DELETE", "/api/v1/admin/domain_blocks/"+url.PathEscape(b.ID), token, nil, false, nil)
		}
	}
	if a.Kind == "unblock_domain" {
		return nil
	}
	return backend(ctx, origin, "POST", "/api/v1/admin/domain_blocks", token, map[string]any{"domain": a.Target, "obfuscate": false, "private_comment": a.Reason}, false, nil)
}
