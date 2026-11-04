package module_site

import (
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"github.com/trentnix/hyperserver/pkg/components/htmx"
	"github.com/trentnix/hyperserver/pkg/ratelimit"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// Routes registers routes with the provided router and, along with Init, satisfies
// the Handler interface
func (m *SiteModule) Routes(mux *routing.Routes) error {
	if m.auth != nil && m.auth.Enabled {
		pages, err := m.auth.PageHandlers(mux)
		if err != nil {
			return err
		}
		mux.Handle("GET /login", pages.Login)
		pages.Submissions.Handle("POST /login", pages.LoginSubmit)
		mux.Handle("GET /register", pages.Registration)
		pages.Submissions.Handle("POST /register", pages.RegistrationSubmit)
	}

	maxClients, err := m.settings.RateLimit.ClientCapacity()
	if err != nil {
		return fmt.Errorf("site.rateLimit.%w", err)
	}
	submissions, err := mux.WithRateLimit(ratelimit.Policy{Requests: 10, Window: time.Minute, MaxClients: maxClients})
	if err != nil {
		return err
	}

	for _, directory := range []string{"css", "js", "img"} {
		files, err := fs.Sub(siteModuleTemplatesFS, "templates/html/"+directory)
		if err != nil {
			return err
		}
		prefix := "/" + directory + "/"
		mux.Handle(prefix, http.StripPrefix(prefix, http.FileServerFS(files)))
	}

	mux.HandleFunc("GET /js/htmx-errors.js", htmx.ServeErrorScript)

	// serve files
	mux.HandleFunc("/favicon.ico", m.ServeFavicon)

	// serve pages
	mux.Handle(homeURL, http.HandlerFunc(m.Home))
	mux.Handle(errorURL, http.HandlerFunc(m.Error))
	mux.Handle(notFoundURL, http.HandlerFunc(m.NotFound))

	// contact
	mux.Handle("GET /contact", http.HandlerFunc(m.GetContact))
	submissions.Handle("POST /contact", http.HandlerFunc(m.Contact))

	// These routes exercise framework features in the local development application.
	// Production applications must not load this module.
	mailPolicy := func(r *http.Request) (bool, error) {
		u := user.GetUserFromContext(r.Context())
		return u != nil && !u.NeedsVerification(), nil
	}
	submissions.Handle("POST /test-email", middleware.RequireAuthorization(mailPolicy)(http.HandlerFunc(m.TestEmail)))
	mux.Handle("POST /session-example", http.HandlerFunc(m.SessionExample))
	mux.WithoutRateLimit().Handle("POST /logout", http.HandlerFunc(m.Logout))
	return mux.Err()
}
