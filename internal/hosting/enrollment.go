package hosting

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/packs/realtime"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	queries "zing/db/queries/gen"
	"zing/internal/installation"
	"zing/internal/social"
	"zing/schema"
)

func enrollmentName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(r)
			if b.Len() >= 2 {
				break
			}
		}
	}
	prefix := b.String()
	if prefix == "" {
		prefix = "zi"
	}
	return prefix + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
}

// SignUp persists enrollment with the auth transaction. It never contacts a
// federation backend before email ownership has been verified.
func SignUp(ctx context.Context, tx pgx.Tx, s auth.SignUp) error {
	if e := queries.New(tx).EnrollmentCreate(ctx, queries.EnrollmentCreateParams{OwnerID: s.Profile.Subject, Username: enrollmentName(s.Profile.Name)}); e != nil {
		return e
	}
	_, e := jobs.From(ctx).EnqueueTx(ctx, tx, "zing.enrollment", s.Profile.Subject, jobs.Unique(s.Profile.Subject))
	return e
}
func AuthEvent(ctx context.Context, event auth.Event) {
	if event.Kind != auth.EventEmailVerified && event.Kind != auth.EventSignedIn {
		return
	}
	// A persisted enrollment and the sweep recover a missed event/enqueue.
	if _, e := queries.New(db.From(ctx)).EnrollmentOwned(ctx, event.Subject); e != nil {
		return
	}
	if _, e := jobs.From(ctx).Enqueue(ctx, "zing.enrollment", event.Subject, jobs.Unique(event.Subject)); e != nil {
		slog.Warn("Automatic account setup will be recovered by the enrollment sweep")
	}
}
func configuredNetworks() []string {
	out := []string{}
	for _, n := range []string{"mastodon", "bluesky"} {
		if _, _, e := configuration(n); e == nil {
			out = append(out, n)
		}
	}
	return out
}
func kickEnrollment(ctx context.Context, owner string, explicit bool) error {
	q := queries.New(db.From(ctx))
	if _, e := q.EnrollmentOwned(ctx, owner); errors.Is(e, pgx.ErrNoRows) {
		return nil
	} else if e != nil {
		return e
	}
	eligible, e := q.EnrollmentEligible(ctx, owner)
	if e != nil || !eligible {
		return e
	}
	p, e := auth.From(ctx).Profile(ctx, owner)
	if e != nil {
		return e
	}
	if installation.Private() && !installation.Allowed(ctx, owner, p.Email) {
		return nil
	}
	for _, n := range configuredNetworks() {
		row, err := q.EnrollmentIdentity(ctx, queries.EnrollmentIdentityParams{OwnerID: owner, Network: n})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if row.State == "active" {
			continue
		}
		failed, err := q.EnrollmentExhausted(ctx, queries.EnrollmentExhaustedParams{OwnerID: owner, Network: n})
		if err != nil {
			return err
		}
		if failed && !explicit {
			continue
		}
		if _, e = jobs.From(ctx).Enqueue(ctx, "zing.provision-identity", schema.HostedProvisionTask{OwnerID: owner, Network: n}, jobs.Unique(owner+":"+n), jobs.MaxAttempts(8)); e != nil {
			return e
		}
	}
	return nil
}
func RetryEnrollment(ctx context.Context, _ *router.Request[router.None]) (schema.OperationResult, error) {
	owner := social.Owner(ctx)
	if _, e := queries.New(db.From(ctx)).EnrollmentOwned(ctx, owner); errors.Is(e, pgx.ErrNoRows) {
		return schema.OperationResult{}, router.NotFound("Enrollment")
	} else if e != nil {
		return schema.OperationResult{}, e
	}
	e := kickEnrollment(ctx, owner, true)
	return schema.OperationResult{Ok: e == nil}, e
}
func RegisterEnrollment(s *lidza.Services) error {
	q := jobs.FromServices(s)
	q.Handle("zing.enrollment", func(ctx context.Context, raw json.RawMessage) error {
		var owner string
		if e := json.Unmarshal(raw, &owner); e != nil {
			return e
		}
		return kickEnrollment(ctx, owner, false)
	})
	q.Handle("zing.provision-identity", func(ctx context.Context, raw json.RawMessage) error {
		var task schema.HostedProvisionTask
		if e := json.Unmarshal(raw, &task); e != nil {
			return e
		}
		if e := task.Validate(); e != nil {
			return e
		}
		source := queries.New(db.From(ctx))
		eligible, e := source.EnrollmentEligible(ctx, task.OwnerID)
		if e != nil || !eligible {
			return e
		}
		name, e := source.EnrollmentOwned(ctx, task.OwnerID)
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		p, e := auth.From(ctx).Profile(ctx, task.OwnerID)
		if e != nil {
			return e
		}
		if installation.Private() && !installation.Allowed(ctx, task.OwnerID, p.Email) {
			return nil
		}
		// A reservation owns its name across crashes and ambiguous upstream outcomes.
		row, e := source.EnrollmentIdentity(ctx, queries.EnrollmentIdentityParams{OwnerID: task.OwnerID, Network: task.Network})
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if row.Username != "" {
			name = row.Username
		}
		if row.State == "rejected" {
			name = enrollmentName(p.Name)
		}
		_, e = createForOwner(ctx, task.OwnerID, schema.HostedIdentityRequest{Network: task.Network, Username: name})
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		hintErr := realtime.From(ctx).Publish(ctx, "user:"+task.OwnerID, map[string]string{"kind": "hosting-updated"})
		if e != nil {
			return e
		}
		return hintErr
	})
	q.Handle("zing.enrollment-sweep", func(ctx context.Context, _ json.RawMessage) error {
		networks := configuredNetworks()
		if len(networks) == 0 {
			return nil
		}
		owners, e := queries.New(db.From(ctx)).EnrollmentCandidates(ctx, networks)
		if e != nil {
			return e
		}
		for _, owner := range owners {
			if e = kickEnrollment(ctx, owner, false); e != nil {
				return e
			}
		}
		return nil
	})
	return q.Schedule("zing.enrollment-sweep", jobs.Every(30*time.Second), nil)
}
func enrollmentView(ctx context.Context, owner string, networks []schema.HostedNetworkView, identities []schema.HostedIdentityView) (*schema.HostedEnrollmentView, error) {
	name, e := queries.New(db.From(ctx)).EnrollmentOwned(ctx, owner)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	out := &schema.HostedEnrollmentView{Username: name, State: "active", Message: "Your configured social identities are ready."}
	enabled, active, failed := 0, 0, false
	for _, n := range networks {
		if !n.Available {
			continue
		}
		enabled++
		for _, h := range identities {
			if h.Network == n.Network {
				if h.State == "active" {
					active++
				}
				if h.State == "failed" || h.State == "rejected" {
					failed = true
				}
			}
		}
	}
	if enabled == 0 {
		out.State = "waiting_configuration"
		out.Message = "Your Zing account is ready. Social identities will be created automatically when the operator configures the hosted networks."
	} else if active < enabled {
		out.State = "provisioning"
		out.Message = "Creating your social identities automatically. You can use each network as it becomes ready."
		if failed {
			out.State = "partial"
			out.Message = "Some social identities need another attempt. Your ready identities remain available."
		}
	}
	return out, nil
}
