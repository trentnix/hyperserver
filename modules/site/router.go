// router.go contains all of the routes and their respective handlers for the module
package module_site

import (
	"net/http"
	"path/filepath"
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
	mux.Handle("GET /test-email", http.HandlerFunc(m.TestEmail))

	// for testing - changes to come
	mux.Handle(authURL, http.HandlerFunc(m.Login))
	mux.Handle(registerURL, http.HandlerFunc(m.Register))
	mux.Handle("/session-example", http.HandlerFunc(m.SessionExample))
	mux.Handle("/logout", http.HandlerFunc(m.Logout))
}
