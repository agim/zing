// Package installation keeps instance policy separate from source distribution.
// All saved settings use Lidza's encrypted PostgreSQL credential store.
package installation

import (
	"context"
	"errors"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/admin"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/credentials"
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/url"
	"strings"
	"zing/schema"
)

func Values() (map[string]string, error) { return env.Values(".") }
func Private() bool {
	v, e := Values()
	return e != nil || v["ZING_ACCESS_MODE"] == "private" || (v["ZING_ACCESS_MODE"] == "" && env.Mode() == "production")
}
func Allowed(ctx context.Context, subject, email string) bool {
	owner, e := auth.From(ctx).FirstSubject(ctx)
	if e != nil {
		return false
	}
	if owner != "" && owner == subject {
		return true
	}
	v, e := Values()
	if e != nil {
		return false
	}
	email = strings.ToLower(strings.TrimSpace(email))
	for _, key := range []string{"ZING_ALLOWED_EMAILS", "ADMIN_USERS"} {
		for _, candidate := range strings.Split(v[key], ",") {
			candidate = strings.TrimSpace(candidate)
			if candidate != "" && (strings.EqualFold(candidate, email) || candidate == subject) {
				return true
			}
		}
	}
	return false
}

// SignUp checks current DB-backed policy for every registration, not only boot.
func SignUp(ctx context.Context, _ pgx.Tx, s auth.SignUp) error {
	if !Private() {
		return nil
	}
	owner, e := auth.From(ctx).FirstSubject(ctx)
	if e != nil {
		return e
	}
	if owner == "" {
		return nil
	}
	if !Allowed(ctx, s.Profile.Subject, s.Profile.Email) {
		return router.Errorf(403, "This Zing instance is private. Ask its operator for access.")
	}
	return nil
}
func Access(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequiresSetup() {
			router.Error(w, 503, "Server setup is required. Open /setup.")
			return
		}
		if Private() {
			p, e := auth.From(r.Context()).Profile(r.Context(), auth.CurrentUser(r.Context()).ID)
			if e != nil {
				router.WriteError(w, r, e)
				return
			}
			if !Allowed(r.Context(), p.Subject, p.Email) {
				router.Error(w, 403, "This Zing instance is private. Ask its operator for access.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func Status(ctx context.Context, _ *router.Request[router.None]) (schema.InstallationStatus, error) {
	v, e := Values()
	if e != nil {
		return schema.InstallationStatus{}, e
	}
	st, e := auth.From(ctx).OwnerClaimStatus(ctx)
	if e != nil && !errors.Is(e, auth.ErrOwnerClaimOff) {
		return schema.InstallationStatus{}, e
	}
	owner, e := auth.From(ctx).FirstSubject(ctx)
	if e != nil {
		return schema.InstallationStatus{}, e
	}
	return schema.InstallationStatus{Configured: v["ZING_SETUP_COMPLETE"] == "true", Private: Private(), OwnerClaimEnabled: st.State != "", OwnerClaimed: owner != ""}, nil
}
func Sections() []admin.Section {
	sections := []admin.Section{{Key: "zing-instance", Title: "Zing instance", Description: "Settings for this installation only. The open-source app supports any domain and hosting provider. Values are saved encrypted in PostgreSQL.", Fields: []admin.Field{{Name: "APP_URL", Label: "Public application origin", Kind: "url", Help: "Your own domain, for example https://social.example.org. The hosting environment can lock this value."}, {Name: "ZING_ACCESS_MODE", Label: "Who can use this instance", Kind: "select", Options: []string{"private", "public"}, Default: "private", Help: "Private allows only the owner and listed email addresses. Public allows self-registration."}, {Name: "ZING_ALLOWED_EMAILS", Label: "Allowed email addresses", Kind: "text", Help: "Comma-separated exact addresses; used in private mode. The owner always retains access."}}}, {Key: "zing-threads", Title: "Threads connection", Optional: true, Description: "Optional. Configure your own approved developer app; leave blank to keep Threads unavailable.", Fields: []admin.Field{{Name: "THREADS_CLIENT_ID", Label: "Threads app ID", Kind: "text"}, {Name: "THREADS_CLIENT_SECRET", Label: "Threads app secret", Kind: "secret"}}}}
	return append(sections, admin.Section{Key: "zing-hosting", Title: "Social server hosting", Optional: true, Description: "Provision identities on your established ActivityPub and PDS servers. Origins and identity domains are permanent once populated. These settings do not install server software.", Fields: []admin.Field{
		{Name: "ZING_ACTIVITYPUB_URL", Label: "ActivityPub server origin", Kind: "url"},
		{Name: "ZING_ACTIVITYPUB_AGENT_URL", Label: "Private provisioning agent origin", Kind: "url"},
		{Name: "ZING_ACTIVITYPUB_AGENT_TOKEN", Label: "Provisioning agent secret", Kind: "secret"},
		{Name: "ZING_PDS_URL", Label: "PDS server origin", Kind: "url"},
		{Name: "ZING_PDS_ADMIN_PASSWORD", Label: "PDS admin password", Kind: "secret"},
		{Name: "ZING_PDS_HANDLE_DOMAIN", Label: "PDS handle domain", Kind: "text"},
		{Name: "ZING_PDS_READ_MODE", Label: "PDS read services", Kind: "select", Options: []string{"local", "appview"}, Default: "local", Help: "AppView mode requires separately configured and verified upstream read services. Local mode reads public hosted repositories only."},
	}})
}
func Page() admin.Page {
	return admin.Page{Name: "First-run setup", Path: "installation", Icon: "settings", Template: "installation.html", Data: func(r *http.Request) (any, error) {
		v, e := Values()
		if e != nil {
			return nil, e
		}
		return map[string]any{"Configured": v["ZING_SETUP_COMPLETE"] == "true", "Private": Private(), "Origin": v["APP_URL"], "Mail": v["MAIL_PROVIDER"], "Storage": v["STORAGE_PROVIDER"]}, nil
	}, Actions: map[string]admin.Action{"complete": complete}}
}
func complete(r *http.Request) (string, error) {
	ctx := r.Context()
	v, e := Values()
	if e != nil {
		return "", e
	}
	origin, e := url.Parse(v["APP_URL"])
	if e != nil || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || (origin.Path != "" && origin.Path != "/") {
		return "", errors.New("set a valid application origin before finishing")
	}
	if origin.Scheme != "https" && !(env.Mode() != "production" && origin.Scheme == "http") {
		return "", errors.New("production requires an HTTPS application origin")
	}
	if v["ZING_ACCESS_MODE"] != "private" && v["ZING_ACCESS_MODE"] != "public" {
		return "", errors.New("choose private or public access")
	}
	switch v["MAIL_PROVIDER"] {
	case "smtp", "mailgun", "sendgrid", "postmark", "resend":
	default:
		return "", errors.New("configure a real mail provider before finishing")
	}
	if v["MAIL_FROM"] == "" {
		return "", errors.New("configure a sender address")
	}
	if v["MAIL_PROVIDER"] != "smtp" && v["MAIL_API_KEY"] == "" {
		return "", errors.New("configure the mail provider API key")
	}
	if v["MAIL_PROVIDER"] == "smtp" && v["MAIL_SMTP_URL"] == "" {
		return "", errors.New("configure the SMTP connection")
	}
	switch v["STORAGE_PROVIDER"] {
	case "local":
		if v["STORAGE_DIR"] == "" {
			return "", errors.New("local storage needs a persistent directory or volume")
		}
	case "s3", "r2", "spaces", "b2", "gcs", "minio":
		if v["STORAGE_BUCKET"] == "" || v["STORAGE_ACCESS_KEY"] == "" || v["STORAGE_SECRET_KEY"] == "" {
			return "", errors.New("configure the object bucket and credentials")
		}
	default:
		return "", errors.New("choose and configure a storage provider")
	}
	store, ok := lidza.Optional[credentials.Store](ctx)
	if !ok {
		return "", errors.New("PostgreSQL credential storage is unavailable")
	}
	// Snapshot explicitly reviewed environment defaults as well as GUI choices.
	// The environment still wins while set; removing an override reuses the DB.
	for _, key := range []string{"AUTH_SECRET", "APP_URL", "ZING_ACCESS_MODE", "ZING_ALLOWED_EMAILS", "MAIL_PROVIDER", "MAIL_FROM", "MAIL_SMTP_URL", "MAIL_API_KEY", "MAIL_DOMAIN", "MAIL_BASE_URL", "STORAGE_PROVIDER", "STORAGE_DIR", "STORAGE_BUCKET", "STORAGE_ENDPOINT", "STORAGE_REGION", "STORAGE_ACCESS_KEY", "STORAGE_SECRET_KEY", "STORAGE_ACCOUNT_ID", "THREADS_CLIENT_ID", "THREADS_CLIENT_SECRET"} {
		if v[key] != "" {
			if e = store.Save(ctx, key, v[key]); e != nil {
				return "", e
			}
		}
	}
	if e = store.Save(ctx, "ZING_SETUP_COMPLETE", "true"); e != nil {
		return "", e
	}
	return "Installation settings saved encrypted in PostgreSQL. Verify email delivery and storage before inviting users.", nil
}

func RequiresSetup() bool {
	v, e := Values()
	if e != nil {
		return true
	}
	required := v["ZING_REQUIRE_SETUP"] == "true" || (v["ZING_REQUIRE_SETUP"] == "" && env.Mode() == "production")
	return required && v["ZING_SETUP_COMPLETE"] != "true"
}

// Setup readiness never converts a dependency outage into a bootstrap marker.
func Readiness(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/readyz" && RequiresSetup() {
			if e := db.From(r.Context()).Ping(r.Context()); e != nil {
				router.Error(w, 503, "Database is unavailable")
				return
			}
			router.Error(w, 503, "Server setup is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
