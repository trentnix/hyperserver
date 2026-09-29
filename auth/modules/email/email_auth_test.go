package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/content"
)

func TestDisabledRegistration(t *testing.T) {
	for _, cfg := range []config.AuthConfig{
		{Enabled: true, RegistrationEnabled: false},
		{Enabled: false, RegistrationEnabled: true},
	} {
		service := &EmailAuthService{config: &config.Config{Auth: cfg}, registerButton: "registration link"}
		if service.GetRegisterButton() != "" {
			t.Error("disabled registration returned a link")
		}
		r := httptest.NewRequest(http.MethodPost, "/auth/register/email", nil)
		w := httptest.NewRecorder()
		service.GetRegister(w, r)
		if w.Code != http.StatusNotFound {
			t.Errorf("registration form returned %d, want 404", w.Code)
		}
		w = httptest.NewRecorder()
		if service.Register(w, r) || w.Code != http.StatusNotFound {
			t.Errorf("registration submission returned %d, want 404", w.Code)
		}
	}
}

func TestInitRequiresAccountEmailTemplates(t *testing.T) {
	for _, tc := range []struct {
		name, path, template string
	}{
		{name: "missing verification", path: emailVerificationTemplate},
		{name: "invalid verification", path: emailVerificationTemplate, template: "{{if}}"},
		{name: "missing reset", path: emailResetTemplate},
		{name: "invalid reset", path: emailResetTemplate, template: "{{if}}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			originalDirectory, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			if err := os.Chdir(root); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Chdir(originalDirectory); err != nil {
					t.Error(err)
				}
			})

			files := map[string]string{
				emailLoginButtonTemplateName:    "Login",
				emailRegisterButtonTemplateName: "Register",
				emailVerificationTemplate:       "Verify {{.Name}}: {{.URL}}",
				emailResetTemplate:              "Reset {{.Name}}: {{.URL}}",
			}
			if tc.template != "" {
				files[tc.path] = tc.template
			} else {
				delete(files, tc.path)
			}
			for path, body := range files {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}

			app := &server.ApplicationServer{
				Config: &config.Config{Auth: config.AuthConfig{
					JwtKey:   "test-key",
					Services: map[string]map[string]string{"email": {"enabled": "true"}},
				}},
				// Init requires a database reference but does not access it.
				Database:       &sqlx.DB{},
				ContentManager: &content.ContentManagerService{},
			}
			service := &EmailAuthService{}
			initErr := service.Init(app)
			cause := errors.Unwrap(initErr)
			want := filepath.Base(tc.path)
			if cause == nil || !strings.Contains(cause.Error(), want) {
				t.Fatalf("Init error = %v, cause = %v, want %s failure", initErr, cause, want)
			}
		})
	}
}
