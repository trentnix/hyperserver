package module_site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/server"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

func TestSiteInitializationPreservesRenderingOverrides(t *testing.T) {
	for _, defaults := range []bool{false, true} {
		app, err := server.NewApplicationServer(config.Config{})
		if err != nil {
			t.Fatal(err)
		}
		cm := app.ContentManager
		if defaults {
			if err := ConfigureRendering(cm, true); err != nil {
				t.Fatal(err)
			}
		}
		files := fstest.MapFS{"custom.html": {Data: []byte("custom layout")}}
		cm.RegisterLayouts(contentservice.PageType, files, "custom.html")
		cm.RegisterLayouts(contentservice.HtmxType, files, "custom.html")
		cm.RegisterComponents(contentservice.PageType, files, "custom.html")
		cm.HomeURL, cm.AuthURL, cm.ErrorURL, cm.NotFoundURL = "/home", "/signin", "/failed", "/missing"
		cm.BuildLayoutData = func(*http.Request) any { return "custom navigation" }
		var calls []string
		cm.HandleMessage = func(http.ResponseWriter, *http.Request, string) { calls = append(calls, "message") }
		cm.HandleError = func(http.ResponseWriter, *http.Request, string, error, int) { calls = append(calls, "error") }
		cm.HandleNotFound = func(http.ResponseWriter, *http.Request) { calls = append(calls, "not-found") }
		cm.LoadNotifications = func(http.ResponseWriter, *http.Request) ([]messages.Notification, error) {
			calls = append(calls, "notifications")
			return nil, nil
		}

		module := Module(nil, nil).New().(*SiteModule)
		module.NewContacts = func(_ context.Context) (ContactRepository, error) { return new(memoryContacts), nil }
		if err := module.Init(t.Context(), app); err != nil {
			t.Fatal(err)
		}
		if cm.HomeURL != "/home" || cm.AuthURL != "/signin" || cm.ErrorURL != "/failed" || cm.NotFoundURL != "/missing" {
			t.Fatal("initialization changed redirect URLs")
		}
		want := []contentservice.TemplatePath{{FS: files, Path: "custom.html"}}
		if !reflect.DeepEqual(cm.Layouts[contentservice.PageType], want) || !reflect.DeepEqual(cm.Layouts[contentservice.HtmxType], want) || !reflect.DeepEqual(cm.Components[contentservice.PageType], want) {
			t.Fatal("initialization changed shared templates")
		}
		w, r := httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)
		if cm.BuildLayoutData(r) != "custom navigation" {
			t.Fatal("initialization replaced navigation data")
		}
		cm.HandleMessage(w, r, "message")
		cm.HandleError(w, r, "error", nil, http.StatusInternalServerError)
		cm.HandleNotFound(w, r)
		if _, err := cm.LoadNotifications(w, r); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(calls, []string{"message", "error", "not-found", "notifications"}) {
			t.Fatalf("initialization replaced callbacks: %v", calls)
		}
	}
}

func TestSiteRenderingDoesNotNeedAnInitializedModule(t *testing.T) {
	cm := contentservice.NewContentManager()
	if err := ConfigureRendering(cm, false); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	cm.HandleError(w, httptest.NewRequest(http.MethodGet, "/", nil), "public failure", nil, http.StatusServiceUnavailable)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "public failure") {
		t.Fatalf("shared error response: %d %s", w.Code, w.Body.String())
	}
	if cm.LoadNotifications != nil {
		t.Fatal("site defaults selected notification storage")
	}
	if err := ConfigureRendering(nil, false); err == nil {
		t.Fatal("accepted missing renderer")
	}
}
