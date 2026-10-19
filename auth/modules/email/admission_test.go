package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestAccountAdmissionFailures(t *testing.T) {
	for _, path := range []string{emailLoginPath, emailRegisterPath} {
		for _, hx := range []bool{false, true} {
			for _, mode := range []string{
				"deny", "error", "allowed with error",
				"parse error", "wrapped parse error", "field errors", "wrapped field errors",
				"canceled", "cancel in callback", "invalid form", "malformed form", "registration disabled",
			} {
				if mode == "registration disabled" && path != emailRegisterPath {
					continue
				}
				t.Run(fmt.Sprintf("%s/htmx=%t/%s", path, hx, mode), func(t *testing.T) {
					var logs bytes.Buffer
					ctx, cancel := context.WithCancel(logger.Set(t.Context(), slog.New(slog.NewJSONHandler(&logs, nil))))
					defer cancel()
					var policyErr error
					switch mode {
					case "error", "allowed with error":
						policyErr = errors.New("private-policy-detail")
					case "parse error", "wrapped parse error":
						policyErr = &form.ParseError{StatusCode: http.StatusBadRequest, Message: "private-policy-detail"}
					case "field errors", "wrapped field errors":
						policyErr = form.FieldErrors{"email": {"private-policy-detail"}}
					}
					if strings.HasPrefix(mode, "wrapped ") {
						policyErr = fmt.Errorf("policy lookup failed: %w", policyErr)
					}

					calls := 0
					options := Options{AllowAccount: func(got context.Context, address string) (bool, error) {
						calls++
						if got != ctx || address != "ADMIN@example.com" {
							t.Fatal("admission lost the request context or checked a different email")
						}
						if mode == "cancel in callback" {
							cancel()
							return true, nil
						}
						return mode == "allowed with error", policyErr
					}}
					accounts := &emailAccountRepository{
						lookup: func(context.Context, string) (*user.User, error) {
							t.Fatal("rejected admission queried accounts")
							return nil, nil
						},
						create: func(context.Context, *user.User) error {
							t.Fatal("rejected admission created an account")
							return nil
						},
					}
					service := Provider(accounts, nil, nil, options).New().(*EmailAuthService)
					service.TemplateFS = emailAuthTemplatesFS
					service.config = &config.Config{Auth: config.AuthConfig{Enabled: true, RegistrationEnabled: mode != "registration disabled"}}
					service.contentManager = content.NewContentManager()
					layouts := fstest.MapFS{"page.html": {Data: []byte(`<!doctype html>{{renderPartial .PartialName .}}`)}, "fragment.html": {Data: []byte(`{{renderPartial .PartialName .}}`)}}
					service.contentManager.AddPageLayout(layouts, "page.html")
					service.contentManager.AddHtmxLayout(layouts, "fragment.html")
					if err := service.prepareTemplates(); err != nil {
						t.Fatal(err)
					}
					fields := url.Values{"email": {"ADMIN@example.com"}, "password": {"TestPassword1!"}, "passwordMatch": {"TestPassword1!"}}
					if mode == "invalid form" {
						fields.Set("email", "invalid")
					}
					body := fields.Encode()
					if mode == "malformed form" {
						body = "email=%zz"
					}
					if mode == "canceled" {
						cancel()
					}
					r := httptest.NewRequest(http.MethodPost, path+"?email=different@example.com", strings.NewReader(body)).WithContext(ctx)
					r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					if hx {
						r.Header.Set("HX-Request", "true")
					}
					w := httptest.NewRecorder()
					if path == emailLoginPath {
						if account := service.Login(w, r); account != nil {
							t.Fatal("rejected admission returned an authenticated account")
						}
					} else {
						service.Register(w, r)
					}
					status, wantCalls := http.StatusOK, 1
					message := "Account access is not available for this email address."
					switch mode {
					case "error", "allowed with error", "canceled", "cancel in callback":
						status, message = http.StatusServiceUnavailable, "Unable to check account access. Please try again."
					case "invalid form":
						message = ""
					case "malformed form":
						status, message = http.StatusBadRequest, ""
					case "registration disabled":
						status, message = http.StatusForbidden, ""
					}
					if policyErr != nil {
						status, message = http.StatusServiceUnavailable, "Unable to check account access. Please try again."
						if !strings.Contains(logs.String(), `"level":"ERROR"`) || !strings.Contains(logs.String(), policyErr.Error()) {
							t.Fatal("admission error was not logged", logs.String())
						}
					}
					if mode == "canceled" || mode == "invalid form" || mode == "malformed form" || mode == "registration disabled" {
						wantCalls = 0
					}
					if w.Code != status || calls != wantCalls || !strings.Contains(w.Body.String(), message) {
						t.Fatalf("status=%d calls=%d body=%s", w.Code, calls, w.Body)
					}
					if w.Header().Get("Location") != "" || w.Header().Get("HX-Redirect") != "" || w.Header().Get("Set-Cookie") != "" || strings.Contains(w.Body.String(), "private-policy-detail") || strings.Contains(w.Body.String(), "TestPassword1!") {
						t.Fatal("rejected admission authenticated, redirected, or disclosed private data", w.Header(), w.Body)
					}
					if message != "" && (!strings.Contains(w.Body.String(), `value="ADMIN@example.com"`) || strings.Contains(w.Body.String(), "<!doctype html>") == hx) {
						t.Fatal("admission failure lost the email or used the wrong layout", w.Body)
					}
				})
			}
		}
	}
}
