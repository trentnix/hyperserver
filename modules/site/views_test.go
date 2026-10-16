package module_site

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/modules/site/models"
	"github.com/trentnix/hyperserver/pkg/server"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

func siteWithViews(t *testing.T, repository ContactRepository) *SiteModule {
	t.Helper()
	cm := contentservice.NewContentManager()
	if err := ConfigureRendering(cm, false); err != nil {
		t.Fatal(err)
	}
	// Expose the selected partial so the contact page and form are distinguishable.
	files := fstest.MapFS{
		"page.html":     {Data: []byte(`<html><title>{{.Title}}</title>{{.PartialName}}:{{renderPartial .PartialName .}}</html>`)},
		"fragment.html": {Data: []byte(`{{.PartialName}}:{{renderPartial .PartialName .}}`)},
	}
	cm.RegisterLayouts(contentservice.PageType, files, "page.html")
	cm.RegisterLayouts(contentservice.HtmxType, files, "fragment.html")
	m := Module(func(context.Context) (ContactRepository, error) { return repository, nil }, nil).New().(*SiteModule)
	if err := m.Init(t.Context(), &server.ApplicationServer{Config: &config.Config{}, ContentManager: cm}); err != nil {
		t.Fatal(err)
	}
	m.registrationEnabled = true
	return m
}

func TestSitePreparedViews(t *testing.T) {
	m := siteWithViews(t, new(memoryContacts))
	for _, tc := range []struct {
		name, path, partial, want string
		handler                   http.HandlerFunc
		status                    int
	}{
		{"home", "/", homePagePartialName, "Welcome!", m.Home, http.StatusOK},
		{"login", "/login", loginPagePartialName, `hx-get="/auth/login/email"`, m.Login, http.StatusOK},
		{"register", "/register", registerPagePartialName, `hx-get="/auth/register/email"`, m.Register, http.StatusOK},
		{"message", "/message", messagePagePartialName, "&lt;public message&gt;", func(w http.ResponseWriter, r *http.Request) {
			m.HandleMessage(w, r, "<public message>")
		}, http.StatusOK},
		{"not-found", "/missing", notFoundPagePartialName, "The resource you requested was not found: /missing", m.NotFound, http.StatusNotFound},
	} {
		for _, variant := range []string{"page", "fragment", "history"} {
			t.Run(tc.name+"/"+variant, func(t *testing.T) {
				t.Parallel()
				r := viewRequest("GET", tc.path, "", variant)
				w := httptest.NewRecorder()
				tc.handler(w, r)
				body := w.Body.String()
				if w.Code != tc.status || !strings.Contains(body, tc.partial+":") || !strings.Contains(body, tc.want) {
					t.Fatalf("response: %d %s", w.Code, body)
				}
				if strings.Contains(body, "<html>") != (variant != "fragment") {
					t.Fatal("wrong page/fragment representation", body)
				}
			})
		}
	}
}

func viewRequest(method, path, body, variant string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if method == "POST" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if variant != "page" {
		r.Header.Set("HX-Request", "true")
	}
	if variant == "history" {
		r.Header.Set("HX-History-Restore-Request", "true")
	}
	return r
}

type viewContacts struct {
	err   error
	calls int
}

func (s *viewContacts) Create(context.Context, *models.ContactSubmission) error {
	s.calls++
	return s.err
}

func TestContactViewSelection(t *testing.T) {
	for _, variant := range []string{"page", "fragment", "history"} {
		for _, outcome := range []string{"initial", "invalid", "success", "storage failure"} {
			t.Run(variant+"/"+outcome, func(t *testing.T) {
				repository := new(viewContacts)
				m := siteWithViews(t, repository)
				layoutCalls := 0
				m.contentManager.BuildLayoutData = func(*http.Request) any { layoutCalls++; return nil }
				method, body, want := "GET", "", `id="contactForm"`
				if outcome != "initial" {
					method = "POST"
					values := url.Values{"name": {"<visitor>"}, "email": {"person@example.com"}, "message": {"Hello"}}
					switch outcome {
					case "invalid":
						values.Set("email", "invalid")
						want = "input-error"
					case "success":
						want = "Your message has been submitted."
					case "storage failure":
						repository.err = errors.New("private storage failure")
						want = "Unable to save your message"
					}
					body = values.Encode()
				}
				w, r := httptest.NewRecorder(), viewRequest(method, "/contact", body, variant)
				if method == "GET" {
					m.GetContact(w, r)
				} else {
					m.Contact(w, r)
				}
				text, partial := w.Body.String(), contactPagePartialName
				if variant == "fragment" {
					partial = contactFormPartialName
				}
				if w.Code != http.StatusOK || !strings.Contains(text, partial+":") || !strings.Contains(text, want) || layoutCalls != 1 {
					t.Fatalf("response: %d layout calls=%d %s", w.Code, layoutCalls, text)
				}
				if strings.Contains(text, "<html>") != (variant != "fragment") || strings.Contains(text, "<title>Contact Us</title>") != (variant != "fragment") {
					t.Fatal("lost contact page selection or title", text)
				}
				if strings.Contains(text, "<visitor>") || strings.Contains(text, "private storage failure") {
					t.Fatal("unescaped input or private failure exposed", text)
				}
				if retained := strings.Contains(text, "&lt;visitor&gt;"); retained != (outcome == "invalid" || outcome == "storage failure") {
					t.Fatal("incorrect submitted field retention", text)
				}
				wantCalls := 0
				if outcome == "success" || outcome == "storage failure" {
					wantCalls = 1
				}
				if repository.calls != wantCalls {
					t.Fatalf("storage calls=%d, want %d", repository.calls, wantCalls)
				}
			})
		}
	}
}

func TestSiteTemplateFailurePrecedesStorage(t *testing.T) {
	for _, variant := range []string{contentservice.PageType, contentservice.HtmxType} {
		t.Run(variant, func(t *testing.T) {
			cm := contentservice.NewContentManager()
			files := fstest.MapFS{"broken.html": {Data: []byte(`{{if}}`)}}
			cm.RegisterLayouts(variant, files, "broken.html")
			opened := false
			m := Module(func(context.Context) (ContactRepository, error) {
				opened = true
				return new(memoryContacts), nil
			}, nil).New().(*SiteModule)
			err := m.Init(t.Context(), &server.ApplicationServer{Config: &config.Config{}, ContentManager: cm})
			if err == nil || opened || m.views != nil || m.errorTemplates != nil {
				t.Fatalf("failed template preparation: error=%v storage opened=%t", err, opened)
			}
		})
	}
}
