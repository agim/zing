package app

import (
	"context"
	"embed"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/cache"
	"net/http"
	"time"
	"zing/internal/atview"
	"zing/internal/chat"
	"zing/internal/hosting"
	"zing/internal/installation"
	"zing/internal/moderation"

	"github.com/agim/lidza/packs/admin"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/packs/realtime"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	"zing/handlers"
	"zing/internal/publishing"
	"zing/internal/social"
	"zing/schema"
)

//go:embed admin
var adminFiles embed.FS

func routes(r *router.Router) {
	router.Route(r, "GET /api/v1/installation", installation.Status)
	router.Route(r, "GET /api/v1/hello/{name}", hello)
	r.HandleFunc("GET /oauth-client-metadata.json", social.BlueskyMetadata)
	r.HandleFunc("GET /api/v1/connections/bluesky/callback", social.BlueskyCallback)
	auth.Mount(r, auth.Options{Title: "Zing", RequireVerified: true, VerifyPath: "/verify", ResetPath: "/reset", OnDeleteUser: deleteUser, OnSignUp: installation.SignUp})
	admin.Mount(r, admin.Options{Title: "Zing", Templates: lidza.Sub(adminFiles, "admin"), Sections: installation.Sections(), Pages: []admin.Page{installation.Page(), moderation.Page()}})
	r.HandleFunc("GET /api/v1/connections/callback", social.Callback)
	appeals := r.Group("/api/v1", auth.Require(), verifiedOnly, installation.Access, accountLimit)
	router.Route(appeals, "POST /api/v1/moderation/appeal", moderation.Appeal)
	g := r.Group("/api/v1", auth.Require(), verifiedOnly, installation.Access, accountLimit, moderation.Require)
	router.Route(g, "POST /api/v1/moderation/reports", report)
	router.Route(g, "GET /api/v1/hosting", hosting.Hosting)
	router.Route(g, "POST /api/v1/hosting/identities", hosting.CreateHostedIdentity)
	router.Route(g, "GET /api/v1/connections", social.Connections)
	router.Route(g, "POST /api/v1/connections", social.Connect)
	router.Route(g, "DELETE /api/v1/connections/{id}", social.Disconnect)
	router.Route(g, "GET /api/v1/feed", social.Feed)
	router.Route(g, "GET /api/v1/search", social.Search)
	router.Route(g, "GET /api/v1/people", social.People)
	router.Route(g, "POST /api/v1/conversations/open", social.OpenConversation)
	router.Route(g, "POST /api/v1/conversations/{id}/accept", social.AcceptZingConversation)
	router.Route(g, "GET /api/v1/profile", social.Profile)
	router.Route(g, "POST /api/v1/actions", social.Action)
	router.Route(g, "GET /api/v1/notifications", social.Notifications)
	router.Route(g, "GET /api/v1/conversations", social.Conversations)
	router.Route(g, "GET /api/v1/messages", social.Messages)
	router.Route(g, "POST /api/v1/messages", social.SendMessage)
	router.Route(g, "GET /api/v1/drafts", publishing.Drafts)
	router.Route(g, "POST /api/v1/drafts", publishing.SaveDraft)
	router.Route(g, "PATCH /api/v1/drafts/{id}", publishing.UpdateDraft)
	router.Route(g, "POST /api/v1/drafts/{id}/publish", publishing.Publish)
	router.Route(g, "POST /api/v1/drafts/{id}/cancel", publishing.Cancel)
	router.Route(g, "GET /api/v1/deliveries", publishing.Deliveries)
	router.Route(g, "POST /api/v1/deliveries/{id}/reconcile", publishing.Reconcile)
	router.Route(g, "POST /api/v1/deliveries/{id}/retry", publishing.Retry)
	router.Route(g, "PUT /api/v1/media", handlers.UploadMedia, router.UploadLimit(8<<20))
	router.Route(g, "PATCH /api/v1/media/{id}", handlers.MediaAlt)
	g.HandleFunc("GET /api/v1/media/{id}", handlers.DownloadMedia)
	g.Handle("GET /api/v1/realtime", realtime.Handler(realtime.Authorize(func(r *http.Request, topic string) bool { return topic == "user:"+social.Owner(r.Context()) })))
}
func deleteUser(ctx context.Context, tx pgx.Tx, subject string) error {
	if e := moderation.DeleteOwner(ctx, tx, subject); e != nil {
		return e
	}
	if e := atview.DeleteOwner(ctx, tx, subject); e != nil {
		return e
	}
	if e := chat.DeleteOwner(ctx, tx, subject); e != nil {
		return e
	}
	if e := hosting.QueueDeletion(ctx, tx, subject); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, subject); e != nil {
		return e
	}
	rows, e := tx.Query(ctx, `SELECT key FROM media WHERE owner_id=$1`, subject)
	if e != nil {
		return e
	}
	keys := []string{}
	for rows.Next() {
		var k string
		if e = rows.Scan(&k); e != nil {
			rows.Close()
			return e
		}
		keys = append(keys, k)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if len(keys) > 0 {
		if _, e = jobs.From(ctx).EnqueueTx(ctx, tx, "zing.delete-media", keys); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, `DELETE FROM mail_message WHERE recipient=(SELECT email FROM auth_user WHERE subject=$1)`, subject); e != nil {
		return e
	}
	for _, table := range []string{"delivery", "draft", "media", "o_auth_attempt", "o_auth_data", "social_connection"} {
		if _, e = tx.Exec(ctx, "DELETE FROM "+table+" WHERE owner_id=$1", subject); e != nil {
			return e
		}
	}
	// Publication jobs keep only a subject and delivery id. Remove those payloads too.
	_, e = tx.Exec(ctx, `DELETE FROM job WHERE kind IN ('zing.publish','zing.sync') AND payload->>'Owner'=$1`, subject)
	return e
}

func hello(ctx context.Context, req *router.Request[router.None]) (schema.Greeting, error) {
	return schema.Greeting{Name: req.Param("name"), Message: "hello, " + req.Param("name")}, nil
}

func verifiedOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := auth.From(r.Context()).Profile(r.Context(), social.Owner(r.Context()))
		if err != nil {
			router.WriteError(w, r, err)
			return
		}
		if !p.Verified() {
			router.Error(w, http.StatusForbidden, "Verify your email first")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func accountLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, e := cache.From(r.Context()).Incr(r.Context(), "api-rate:"+social.Owner(r.Context()), 1, time.Minute)
		if e != nil {
			router.WriteError(w, r, e)
			return
		}
		if n > 180 {
			w.Header().Set("Retry-After", "60")
			router.Error(w, 429, "Too many requests. Try again in a minute.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func report(ctx context.Context, req *router.Request[schema.ReportRequest]) (schema.OperationResult, error) {
	s, e := social.Load(ctx, social.Owner(ctx), req.Body.ConnectionID)
	if e != nil {
		return schema.OperationResult{}, e
	}
	return moderation.CreateReport(ctx, social.Owner(ctx), s, req.Body)
}
