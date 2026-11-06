package app

import (
	"context"
	"encoding/json"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/packs/storage"
	"github.com/agim/lidza/pkg/middleware"
	"zing/internal/atview"
	"zing/internal/chat"
	"zing/internal/hosting"
	"zing/internal/installation"
	"zing/internal/moderation"
	"zing/internal/publishing"
	"zing/internal/social"
)

func onStart(ctx context.Context, s *lidza.Services) error {
	publishing.Register(s)
	hosting.Register(s)
	if e := hosting.RegisterEnrollment(s); e != nil {
		return e
	}
	chat.Register(s)
	moderation.Register(s)
	atview.Register(s)
	if e := social.RegisterSync(s); e != nil {
		return e
	}
	jobs.FromServices(s).Handle("zing.delete-media", func(ctx context.Context, raw json.RawMessage) error {
		var keys []string
		if e := json.Unmarshal(raw, &keys); e != nil {
			return e
		}
		for _, key := range keys {
			if e := storage.From(ctx).Delete(ctx, key); e != nil {
				return e
			}
		}
		return nil
	})
	return nil
}
func appMiddleware() []middleware.Middleware { return []middleware.Middleware{installation.Readiness} }
