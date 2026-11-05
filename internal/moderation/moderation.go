// Package moderation owns reports and audited, durable native-server actions.
package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
	queries "zing/db/queries/gen"
	"zing/internal/chat"
	"zing/internal/platform/vault"
	"zing/internal/providers"
	"zing/schema"
)

func Check(ctx context.Context, owner string) error {
	suspended, e := queries.New(db.From(ctx)).ModerationSuspended(ctx, owner)
	if e != nil {
		return e
	}
	if suspended {
		return router.ErrorCode(403, "account_suspended", "Your Zing account is suspended. You can appeal in Settings or delete your account.")
	}
	return nil
}
func Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := Check(r.Context(), auth.CurrentUser(r.Context()).ID); e != nil {
			router.WriteError(w, r, e)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func CreateReport(ctx context.Context, owner string, s providers.Session, in schema.ReportRequest) (schema.OperationResult, error) {
	out := schema.OperationResult{}
	target := in.Target
	targetOwner := ""
	evidence := ""
	switch in.Kind {
	case "message":
		if !s.Hosted || s.Network != "bluesky" || (in.MessageID == nil || *in.MessageID == "") {
			return out, router.Errorf(422, "Only selected local Zing chat messages can be attached here")
		}
		ev, e := chat.Evidence(ctx, owner, s.Subject, *in.MessageID)
		if e != nil {
			return out, e
		}
		if ev.Owner == owner {
			return out, router.Errorf(422, "Select a received message")
		}
		target = ev.Sender
		targetOwner = ev.Owner
		b, e := json.Marshal(map[string]string{"messageId": *in.MessageID, "sender": ev.Sender, "text": ev.Text})
		if e != nil {
			return out, e
		}
		evidence = string(b)
	case "account", "post":
		subject := target
		if in.Kind == "post" {
			post, e := s.Post(ctx, target)
			if e != nil {
				return out, e
			}
			subject = post.Author.ID
			b, e := json.Marshal(post)
			if e != nil {
				return out, e
			}
			evidence = string(b)
		} else {
			account, e := s.Profile(ctx, target)
			if e != nil {
				return out, e
			}
			subject = account.ID
			b, e := json.Marshal(account)
			if e != nil {
				return out, e
			}
			evidence = string(b)
		}
		if s.Hosted {
			var e error
			targetOwner, e = queries.New(db.From(ctx)).ModerationTargetOwner(ctx, queries.ModerationTargetOwnerParams{Network: s.Network, Server: s.Server, Subject: subject})
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return out, e
			}
		}
	default:
		return out, router.Errorf(422, "Choose an account, post, or an explicitly attached chat message")
	}
	sealed, e := vault.Seal(evidence)
	if e != nil {
		return out, e
	}
	_, e = queries.New(db.From(ctx)).ModerationCreateReport(ctx, queries.ModerationCreateReportParams{ReporterID: owner, Network: s.Network, Target: target, TargetOwnerID: targetOwner, Reason: strings.TrimSpace(in.Reason), EvidenceSealed: sealed, Kind: in.Kind})
	return schema.OperationResult{Ok: e == nil}, e
}
func Appeal(ctx context.Context, req *router.Request[schema.AppealRequest]) (schema.OperationResult, error) {
	out := schema.OperationResult{}
	owner := auth.CurrentUser(ctx).ID
	tx, e := db.From(ctx).Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	q := queries.New(tx)
	if e = q.ChatLock(ctx, "appeal:"+owner); e != nil {
		return out, e
	}
	suspended, e := q.ModerationSuspended(ctx, owner)
	if e != nil {
		return out, e
	}
	if !suspended {
		return out, router.Errorf(409, "Your account is not suspended")
	}
	if _, e = q.ModerationOpenAppeal(ctx, owner); e == nil {
		return schema.OperationResult{Ok: true}, nil
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	_, e = q.ModerationCreateReport(ctx, queries.ModerationCreateReportParams{ReporterID: owner, Network: "zing", Target: owner, TargetOwnerID: owner, Reason: req.Body.Reason, EvidenceSealed: "", Kind: "appeal"})
	if e != nil {
		return out, e
	}
	e = tx.Commit(ctx)
	return schema.OperationResult{Ok: e == nil}, e
}
func DeleteOwner(ctx context.Context, tx pgx.Tx, owner string) error {
	q := queries.New(tx)
	if e := q.ChatLock(ctx, "moderation-account:"+owner); e != nil {
		return e
	}
	if e := q.ModerationCancelOwnerActions(ctx, owner); e != nil {
		return e
	}
	if e := q.ModerationDeleteReporterData(ctx, owner); e != nil {
		return e
	}
	if e := q.ModerationAnonymizeDeletedTarget(ctx, owner); e != nil {
		return e
	}
	return q.ModerationDeleteOwnerState(ctx, owner)
}
