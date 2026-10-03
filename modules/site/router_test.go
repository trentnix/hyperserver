package module_site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/server"
	content_services "github.com/trentnix/hyperserver/pkg/services/content"
)

func siteForRouteTest(t *testing.T) (*SiteModule, *http.ServeMux) {
	t.Helper()
	s := &server.ApplicationServer{
		Config:         &config.Config{Auth: config.AuthConfig{Enabled: true, RegistrationEnabled: true}},
		ContentManager: content_services.NewContentManager(),
	}
	m := new(SiteModule)
	db, err := sqlx.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	s.Database = db
	t.Cleanup(func() { s.Shutdown() })
	if err := m.Init(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	// Avoid loading templates or sessions when the site's catch-all reports a 404.
	s.ContentManager.HandleNotFound = http.NotFound
	mux := http.NewServeMux()
	m.Routes(mux)
	return m, mux
}

func TestSiteRegistersSampleRoutes(t *testing.T) {
	m, mux := siteForRouteTest(t)
	if m.contentManager.AuthURL != authURL {
		t.Fatalf("AuthURL = %q, want %q", m.contentManager.AuthURL, authURL)
	}
	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/test-email"},
		{http.MethodPost, "/session-example"},
		{http.MethodGet, "/login"},
		{http.MethodGet, "/register"},
		{http.MethodPost, "/logout"},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			_, pattern := mux.Handler(httptest.NewRequest(route.method, route.path, nil))
			if want := route.method + " " + route.path; pattern != want {
				t.Errorf("pattern = %q, want %q", pattern, want)
			}
		})
	}
}

func TestSampleMutationsRequirePost(t *testing.T) {
	_, mux := siteForRouteTest(t)
	for _, path := range []string{"/test-email", "/session-example", "/logout"} {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
			t.Run(method+" rejected "+path, func(t *testing.T) {
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, httptest.NewRequest(method, path, nil))
				if w.Code != http.StatusNotFound {
					t.Errorf("status = %d, want 404 from catch-all", w.Code)
				}
			})
		}
	}
}

func TestRegularSiteRoutesRemainRegistered(t *testing.T) {
	_, mux := siteForRouteTest(t)
	for _, route := range []struct{ method, path, pattern string }{
		{http.MethodGet, "/", "/"},
		{http.MethodGet, "/contact", "GET /contact"},
		{http.MethodPost, "/contact", "POST /contact"},
		{http.MethodGet, "/error", "/error"},
		{http.MethodGet, "/404", "/404"},
		{http.MethodGet, "/css/site.css", "/css/"},
	} {
		_, pattern := mux.Handler(httptest.NewRequest(route.method, route.path, nil))
		if pattern != route.pattern {
			t.Errorf("%s %s: pattern = %q, want %q", route.method, route.path, pattern, route.pattern)
		}
	}
}
