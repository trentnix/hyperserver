package form

import (
	"bytes"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

type failingErrorForm struct {
	Form
	err error
}

func (f *failingErrorForm) Fail() (string, error) { return "", f.err }

type failedFormResponse struct {
	*httptest.ResponseRecorder
	statuses []int
	writes   int
	err      error
}

func (w *failedFormResponse) WriteHeader(status int) {
	w.statuses = append(w.statuses, status)
	w.ResponseRecorder.WriteHeader(status)
}

func (w *failedFormResponse) Write(body []byte) (int, error) {
	w.writes++
	n, _ := w.ResponseRecorder.Write(body[:len(body)/2])
	return n, w.err
}

func TestFormDeliveryFailureDoesNotWriteErrorResponse(t *testing.T) {
	for _, hx := range []bool{false, true} {
		for _, policy := range []string{"custom", "no callback", "no manager"} {
			for _, cause := range []error{errors.New("connection lost"), nil} {
				t.Run(fmt.Sprintf("htmx=%t/%s/error=%v", hx, policy, cause), func(t *testing.T) {
					var logs bytes.Buffer
					r := httptest.NewRequest(http.MethodPost, "/", nil)
					r.Header.Set("HX-Request", fmt.Sprint(hx))
					r = r.WithContext(logger.Set(r.Context(), slog.New(slog.NewTextHandler(&logs, nil))))
					cm := contentservice.NewContentManager()
					if policy == "custom" {
						cm.HandleError = func(http.ResponseWriter, *http.Request, string, error, int) {
							t.Fatal("delivery failure called the error-page handler")
						}
					}
					page := content.NewManagedContent(r, cm)
					if policy == "no manager" {
						page = content.NewContent(r)
					}
					page.AddContent(fstest.MapFS{"form.html": {Data: []byte("<p>form contents</p>")}}, "form.html")
					page.ResponseStatusCode = http.StatusUnprocessableEntity
					w := &failedFormResponse{ResponseRecorder: httptest.NewRecorder(), err: cause}
					HandleFormError(w, r, page, &Form{}, "", nil)

					if w.writes != 1 || len(w.statuses) != 1 || w.statuses[0] != http.StatusUnprocessableEntity {
						t.Fatalf("writes=%d, statuses=%v", w.writes, w.statuses)
					}
					if cause == nil {
						cause = io.ErrShortWrite
					}
					if strings.Count(logs.String(), cause.Error()) != 1 {
						t.Fatalf("delivery failure must be logged once: %s", logs.String())
					}
				})
			}
		}
	}
}

func TestFormRenderingFailurePresentation(t *testing.T) {
	for _, hx := range []bool{false, true} {
		for _, policy := range []string{"custom", "no callback", "no manager"} {
			for _, failure := range []string{"template", "field errors", "notifications"} {
				if failure == "notifications" && policy == "no manager" {
					continue
				}
				t.Run(fmt.Sprintf("htmx=%t/%s/%s", hx, policy, failure), func(t *testing.T) {
					var logs bytes.Buffer
					r := httptest.NewRequest(http.MethodPost, "/", nil)
					r.Header.Set("HX-Request", fmt.Sprint(hx))
					r = r.WithContext(logger.Set(r.Context(), slog.New(slog.NewTextHandler(&logs, nil))))
					cause := errors.New("private rendering detail")
					cm := contentservice.NewContentManager()
					w := httptest.NewRecorder()
					calls := 0
					message := "Unable to display <form>"
					wantMessage := message
					var formErr error
					if failure == "field errors" {
						formErr = FieldErrors{"Name": {"Required"}}
						wantMessage = http.StatusText(http.StatusInternalServerError)
					}
					if policy == "custom" {
						cm.HandleError = func(got http.ResponseWriter, request *http.Request, message string, err error, status int) {
							calls++
							if got != w || request != r || message != wantMessage || status != http.StatusInternalServerError || !errors.Is(err, cause) {
								t.Fatalf("callback arguments: message=%q error=%v status=%d", message, err, status)
							}
							if logs.Len() != 0 || w.Body.Len() != 0 || w.Header().Get("HX-Retarget") != "" {
								t.Fatal("rendering failure was already logged or written")
							}
							logger.LogRequestError(request, err)
							w.Header().Set("Content-Type", "text/html; charset=utf-8")
							w.Header().Set("X-Error-Policy", "custom")
							w.WriteHeader(status)
							fmt.Fprint(w, "<p>"+html.EscapeString(message)+"</p>")
						}
					}
					page := content.NewContent(r)
					if policy != "no manager" {
						page = content.NewManagedContent(r, cm)
					}
					template := "<p>partial output</p>{{.Data.Fail}}"
					if failure == "notifications" {
						template = "<p>must not render</p>"
						cm.LoadNotifications = func(*http.Request) ([]messages.Notification, func(http.ResponseWriter) error, error) {
							return nil, nil, cause
						}
					}
					page.AddContent(fstest.MapFS{"form.html": {Data: []byte(template)}}, "form.html")
					page.ResponseStatusCode = http.StatusUnprocessableEntity
					page.Headers = map[string]string{"HX-Retarget": "#form", "Content-Length": "999"}
					HandleFormError(w, r, page, &failingErrorForm{err: cause}, message, formErr)

					wantBody, wantType, wantCalls := wantMessage+"\n", "text/plain; charset=utf-8", 0
					if policy == "custom" {
						wantBody, wantType, wantCalls = "<p>"+html.EscapeString(wantMessage)+"</p>", "text/html; charset=utf-8", 1
					}
					if w.Code != http.StatusInternalServerError || w.Body.String() != wantBody || w.Result().Header.Get("Content-Type") != wantType || calls != wantCalls {
						t.Fatalf("response: status=%d body=%q headers=%v callback calls=%d", w.Code, w.Body.String(), w.Result().Header, calls)
					}
					if strings.Count(logs.String(), cause.Error()) != 1 {
						t.Fatalf("rendering failure must be logged once: %s", logs.String())
					}
					if w.Result().Header.Get("HX-Retarget") != "" || w.Result().Header.Get("Content-Length") != "" {
						t.Fatal("failed form response headers reached the error response")
					}
				})
			}
		}
	}
}

func TestFormParserRejectionBypassesErrorPolicy(t *testing.T) {
	for _, hx := range []bool{false, true} {
		for _, status := range []int{http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType, http.StatusInternalServerError} {
			t.Run(fmt.Sprintf("htmx=%t/status=%d", hx, status), func(t *testing.T) {
				r := httptest.NewRequest(http.MethodPost, "/", nil)
				r.Header.Set("HX-Request", fmt.Sprint(hx))
				cm := contentservice.NewContentManager()
				cm.HandleError = func(http.ResponseWriter, *http.Request, string, error, int) {
					t.Fatal("parser rejection called the error policy")
				}
				cm.LoadNotifications = func(*http.Request) ([]messages.Notification, func(http.ResponseWriter) error, error) {
					t.Fatal("parser rejection tried to render")
					return nil, nil, nil
				}
				w := httptest.NewRecorder()
				parseErr := &ParseError{StatusCode: status, Message: "Rejected form"}
				// A nil form also ensures the rejection never touches submitted values.
				HandleFormError(w, r, content.NewManagedContent(r, cm), nil, "private submitted value", fmt.Errorf("parse: %w", parseErr))
				if w.Code != status || w.Body.String() != "Rejected form\n" || w.Result().Header.Get("Content-Type") != "text/plain; charset=utf-8" {
					t.Fatalf("rejection: status=%d body=%q headers=%v", w.Code, w.Body.String(), w.Result().Header)
				}
			})
		}
	}
}
