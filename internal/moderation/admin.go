package moderation

import (
	"context"
	"github.com/agim/lidza/packs/admin"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
	"golang.org/x/net/idna"
	"net/http"
	"strings"
	queries "zing/db/queries/gen"
	"zing/internal/platform/vault"
)

func verifiedStaff(ctx context.Context) error {
	// These callbacks are mounted only behind the admin pack's current allowlist
	// and CSRF gate. Also require a verified, unsuspended account at execution.
	u := auth.CurrentUser(ctx)
	if u == nil {
		return router.Errorf(401, "Sign in")
	}
	p, e := auth.From(ctx).Profile(ctx, u.ID)
	if e != nil {
		return e
	}
	if !p.Verified() {
		return router.Errorf(403, "Verify your email")
	}
	return Check(ctx, u.ID)
}
func Page() admin.Page {
	return admin.Page{Name: "Moderation", Path: "moderation", Icon: "shield", Template: "moderation.html", Data: pageData, Actions: map[string]admin.Action{"action": action, "resolve": resolve}}
}
func pageData(r *http.Request) (any, error) {
	ctx := r.Context()
	if e := verifiedStaff(ctx); e != nil {
		return nil, e
	}
	q := queries.New(db.From(ctx))
	users, e := q.ModerationUsers(ctx)
	if e != nil {
		return nil, e
	}
	reports, e := q.ModerationReports(ctx)
	if e != nil {
		return nil, e
	}
	actions, e := q.ModerationActions(ctx)
	if e != nil {
		return nil, e
	}
	evidence := ""
	if id := r.URL.Query().Get("report"); id != "" {
		report, err := q.ModerationReport(ctx, id)
		if err != nil {
			return nil, err
		}
		if report.EvidenceSealed != "" {
			evidence, e = vault.Open(report.EvidenceSealed)
			if e != nil {
				return nil, e
			}
		}
	}
	destinations := []queries.ModerationDestination{}
	if id := r.URL.Query().Get("action"); id != "" {
		destinations, e = q.ModerationDestinations(ctx, id)
		if e != nil {
			return nil, e
		}
	}
	return map[string]any{"Users": users, "Reports": reports, "Actions": actions, "Evidence": evidence, "Destinations": destinations, "RequestKey": uuid.NewString()}, nil
}
func resolve(r *http.Request) (string, error) {
	if e := verifiedStaff(r.Context()); e != nil {
		return "", e
	}
	_, e := queries.New(db.From(r.Context())).ModerationResolveReport(r.Context(), r.Form.Get("report"))
	return "Report resolved", e
}
func action(r *http.Request) (string, error) {
	ctx := r.Context()
	if e := verifiedStaff(ctx); e != nil {
		return "", e
	}
	kind, target, reason := r.Form.Get("kind"), strings.TrimSpace(r.Form.Get("target")), strings.TrimSpace(r.Form.Get("reason"))
	network := ""
	key := r.Form.Get("requestKey")
	if _, e := uuid.Parse(key); e != nil {
		return "", router.Errorf(422, "Reload the moderation page")
	}
	if len(reason) < 1 || len(reason) > 2000 {
		return "", router.Errorf(422, "Record a reason (1–2000 characters)")
	}
	tx, e := db.From(ctx).Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	q := queries.New(tx)
	switch kind {
	case "suspend", "restore":
		network = "all"
		if e = q.ChatLock(ctx, "moderation-account:"+target); e != nil {
			return "", e
		}
		exists, err := q.ModerationOwnerExists(ctx, target)
		if err != nil {
			return "", err
		}
		if !exists {
			return "", router.NotFound("User")
		}
		first, err := auth.From(ctx).FirstSubject(ctx)
		if err != nil {
			return "", err
		}
		if target == first || target == auth.CurrentUser(ctx).ID {
			return "", router.Errorf(422, "The instance owner and your own account are protected")
		}
	case "remove_post":
		report, err := q.ModerationReport(ctx, r.Form.Get("report"))
		if err != nil {
			return "", err
		}
		if report.Kind != "post" || report.TargetOwnerID == "" {
			return "", router.Errorf(422, "Only a reported post hosted here can be removed")
		}
		network = report.Network
		target = report.Target
	case "block_domain", "unblock_domain":
		network = "mastodon"
		ascii, err := idna.Lookup.ToASCII(target)
		if err != nil {
			return "", router.Errorf(422, "Invalid domain")
		}
		target = strings.ToLower(ascii)
		if len(target) > 253 || !strings.Contains(target, ".") || strings.ContainsAny(target, "/:@?# ") {
			return "", router.Errorf(422, "Enter a hostname, without a URL or path")
		}
	default:
		return "", router.Errorf(422, "Unsupported moderation action")
	}
	id, e := q.ModerationCreateAction(ctx, queries.ModerationCreateActionParams{ModeratorID: auth.CurrentUser(ctx).ID, Kind: kind, Network: network, Target: target, Reason: reason, RequestKey: key})
	if e != nil {
		return "", e
	}
	row, e := q.ModerationAction(ctx, id)
	if e != nil {
		return "", e
	}
	if row.ModeratorID != auth.CurrentUser(ctx).ID || row.Kind != kind || row.Network != network || row.Target != target || row.Reason != reason {
		return "", router.Errorf(409, "This request identifier was already used for another action")
	}
	if row.State != "pending" {
		return "Action already recorded; see its delivery status", nil
	}
	if kind == "suspend" {
		if e = q.ModerationSetSuspended(ctx, queries.ModerationSetSuspendedParams{OwnerID: target, Suspended: true, Reason: reason}); e != nil {
			return "", e
		}
	}
	if _, e = jobs.From(ctx).EnqueueTx(ctx, tx, "zing.moderate", id, jobs.Unique("moderate:"+id)); e != nil {
		return "", e
	}
	e = tx.Commit(ctx)
	return "Action queued. Backend confirmation and failures appear below.", e
}
