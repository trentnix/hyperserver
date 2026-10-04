package module_site

import (
	"net/http"
	"path/filepath"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// Routes registers routes with the provided router and, along with Init, satisfies
// the Handler interface
func (m *SiteModule) Routes(mux *http.ServeMux) {
	// media paths
	cssPath := filepath.Join("modules", "site", "templates", "html", "css")
	jsPath := filepath.Join("modules", "site", "templates", "html", "js")
	imgPath := filepath.Join("modules", "site", "templates", "html", "img")

	// serve media
	mux.Handle("/css/", http.StripPrefix("/css/", http.FileServer(http.Dir(cssPath)))) // Serve .css files
	mux.Handle("/js/", http.StripPrefix("/js/", http.FileServer(http.Dir(jsPath))))    // Serve .js files
	mux.Handle("/img/", http.StripPrefix("/img/", http.FileServer(http.Dir(imgPath)))) // Serve image files

	// serve files
	mux.HandleFunc("/favicon.ico", m.ServeFavicon)

	// serve pages
	mux.Handle(homeURL, http.HandlerFunc(m.Home))
	mux.Handle(errorURL, http.HandlerFunc(m.Error))
	mux.Handle(notFoundURL, http.HandlerFunc(m.NotFound))

	// contact
	mux.Handle("GET /contact", http.HandlerFunc(m.GetContact))
	mux.Handle("POST /contact", http.HandlerFunc(m.Contact))

	// These routes exercise framework features in the local development application.
	// Production applications must not load this module.
	mailPolicy := func(r *http.Request) (bool, error) {
		u := user.GetUserFromContext(r.Context())
		return u != nil && !u.NeedsVerification(), nil
	}
	mux.Handle("POST /test-email", middleware.RequireAuthorization(mailPolicy)(http.HandlerFunc(m.TestEmail)))
	mux.Handle("GET "+authURL, http.HandlerFunc(m.Login))
	if m.registrationEnabled {
		mux.Handle("GET "+registerURL, http.HandlerFunc(m.Register))
	} else {
		mux.HandleFunc(registerURL, auth.RegistrationUnavailable)
	}
	mux.Handle("POST /session-example", http.HandlerFunc(m.SessionExample))
	mux.Handle("POST /logout", http.HandlerFunc(m.Logout))
}
