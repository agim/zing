package app

import (
	"io/fs"

	"github.com/agim/lidza"
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/middleware"
)

// New describes the application; main supplies the built frontend and tests
// pass nil to exercise the API without the frontend build.
func New(dist fs.FS) lidza.App {
	return lidza.App{
		Name:       "zing",
		CSP:        contentSecurityPolicy(),
		Dist:       dist,
		Routes:     routes,
		Packs:      packs(),
		Tools:      tools(),
		OnStart:    onStart,
		Middleware: appMiddleware(),
	}
}

func contentSecurityPolicy() string {
	if env.Mode() == "dev" {
		return ""
	}
	return middleware.AddCSP(middleware.DefaultCSP, "img-src", "https:")
}
