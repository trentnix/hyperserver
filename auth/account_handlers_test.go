package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

type accountActionsTestProvider struct {
	capabilityProvider
	actions []AccountAction
}

func (p *accountActionsTestProvider) AccountActions(*user.User) []AccountAction { return p.actions }

func TestAccountActions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		actions []AccountAction
		policy  Policy
		status  int
		want    string
	}{
		{name: "escaped labels", actions: []AccountAction{{Label: "<script>unsafe</script>", URL: "/account-action"}}, status: 200, want: "&lt;script&gt;unsafe&lt;/script&gt;"},
		{name: "no controls", status: 200, want: "No account controls"},
		{name: "external URL", actions: []AccountAction{{Label: "External", URL: "https://example.com"}}, status: 500, want: "Account controls are temporarily unavailable"},
		{name: "missing URL", actions: []AccountAction{{Label: "Missing"}}, status: 500, want: "Account controls are temporarily unavailable"},
		{name: "missing label", actions: []AccountAction{{URL: "/local"}}, status: 500, want: "Account controls are temporarily unavailable"},
		{name: "denied", policy: Policy{AllowAccount: func(context.Context, string) (bool, error) { return false, nil }}, status: 403, want: "not allowed"},
		{name: "policy unavailable", policy: Policy{AllowAccount: func(context.Context, string) (bool, error) { return false, errors.New("private failure") }}, status: 503, want: "temporarily unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cm := contentservice.NewContentManager()
			cm.HandleError = func(w http.ResponseWriter, r *http.Request, message string, _ error, status int) {
				http.Error(w, message, status)
			}
			layout := fstest.MapFS{"page.html": {Data: []byte(`{{renderPartial .PartialName .}}`)}}
			cm.AddPageLayout(layout, "page.html")
			cm.AddHtmxLayout(layout, "page.html")
			manager := &AuthManager{contentManager: cm, Policy: tc.policy, Services: &Registry{services: []AuthService{&accountActionsTestProvider{actions: tc.actions}, &capabilityProvider{name: "no account actions"}}}}
			if err := manager.prepareTemplates(); err != nil {
				t.Fatal(err)
			}
			for _, hx := range []bool{false, true} {
				r := httptest.NewRequest("GET", "/auth/account", nil)
				if hx {
					r.Header.Set("HX-Request", "true")
				}
				r = user.AddUserToRequestContext(r, &user.User{Email: "reader@example.com", Verified: true, Password: "secret-hash"})
				w := httptest.NewRecorder()
				manager.GetAccount(w, r)
				if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.want) {
					t.Fatalf("status=%d body=%s", w.Code, w.Body)
				}
				if strings.Contains(w.Body.String(), "secret-hash") || strings.Contains(w.Body.String(), "private failure") || strings.Contains(w.Body.String(), "<script>") {
					t.Fatal("unsafe account response")
				}
			}
		})
	}
}

func TestAccountPageRequiresIdentity(t *testing.T) {
	manager := &AuthManager{contentManager: contentservice.NewContentManager()}
	manager.contentManager.HandleError = func(w http.ResponseWriter, r *http.Request, message string, _ error, status int) {
		http.Error(w, message, status)
	}
	w := httptest.NewRecorder()
	manager.GetAccount(w, httptest.NewRequest("GET", "/auth/account", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d", w.Code)
	}
}
