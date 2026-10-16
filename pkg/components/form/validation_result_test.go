package form

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/trentnix/hyperserver/pkg/components/content"
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

func TestValidationErrorsBlockOperationsAndRenderSafely(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		for _, reason := range []string{"fields", "parse", "binding", "binding fields", "wrapped binding fields", "valid"} {
			t.Run(fmt.Sprintf("htmx=%t/%s", htmx, reason), func(t *testing.T) {
				var logs bytes.Buffer
				body := "value="
				if reason == "valid" || reason == "binding" {
					body = "value=valid"
				}
				if reason == "parse" {
					body = "value=%zz"
				}
				r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if htmx {
					r.Header.Set("HX-Request", "true")
				}
				r = r.WithContext(logger.Set(r.Context(), slog.New(slog.NewJSONHandler(&logs, nil))))
				f := &testForm{}
				if reason == "binding" {
					f.bindErr = hs_errors.WithCause(ErrInvalidForm, errors.New("private-storage-detail"))
				}
				bindingFields := reason == "binding fields" || reason == "wrapped binding fields"
				if bindingFields {
					f.bindErr = FieldErrors{"Value": {"Enter a whole number."}}
					if reason == "wrapped binding fields" {
						f.bindErr = fmt.Errorf("bind value: %w", f.bindErr)
					}
				}
				page := content.NewContent(r)
				page.AddContent(fstest.MapFS{"form.html": {Data: []byte(`{{template "form" .}}{{define "form"}}{{range .Data.GetAllFieldErrors}}<p>{{.}}</p>{{end}}{{range .Data.GetErrorMessages}}<p>{{.Message}}</p>{{end}}{{end}}`)}}, "form.html")
				page.PartialName = "form"
				persisted, mailed := 0, 0
				w := httptest.NewRecorder()
				handle := func() {
					if err := ParseAndValidate(r, f); err != nil {
						if reason == "wrapped binding fields" && !errors.Is(err, f.bindErr) {
							t.Fatal("binding error wrapper was lost")
						}
						HandleFormError(w, r, page, f, "Unable to process submission", err)
						return
					}
					persisted++
					mailed++
				}
				handle()
				want := 0
				if reason == "valid" {
					want = 1
				}
				if persisted != want || mailed != want {
					t.Fatal("invalid input reached an operation")
				}
				if strings.Contains(w.Body.String(), "private-storage-detail") {
					t.Fatal("private cause reached HTTP output")
				}
				if reason == "binding" && !strings.Contains(logs.String(), "private-storage-detail") {
					t.Fatal("private cause missing from diagnostics")
				}
				if reason == "fields" {
					if logs.Len() != 0 || !strings.Contains(w.Body.String(), RequiredMessage) || strings.Contains(w.Body.String(), "Unable to process submission") {
						t.Fatalf("invalid field response or diagnostics: %s / %s", w.Body.String(), logs.String())
					}
				}
				if bindingFields {
					if f.checks != 0 || f.IsValidated() {
						t.Fatal("binding failure ran validation")
					}
					if w.Code != http.StatusOK || logs.Len() != 0 || !strings.Contains(w.Body.String(), "Enter a whole number.") || strings.Contains(w.Body.String(), "Unable to process submission") {
						t.Fatalf("binding field response or diagnostics: %s / %s", w.Body.String(), logs.String())
					}
				}
				if reason == "parse" && w.Code != http.StatusBadRequest {
					t.Fatal("parse failure rendered a form")
				}
			})
		}
	}
}
