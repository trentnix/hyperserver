package main

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestHTTPDefaultBodyLimits(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		read := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, err := io.Copy(io.Discard, r.Body)
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				http.Error(w, "Body too large", http.StatusRequestEntityTooLarge)
				return
			}
			if err != nil {
				http.Error(w, "Body read failed", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
		// A newly registered handler needs no annotation to get the default limit.
		routes := routing.NewRoutes(h.app.Web)
		routes.Handle("POST /test/body", read)
		routes.HandleWithBodyLimit("POST /test/body/larger", read, 2*routing.DefaultMaxBodyBytes)
		routes.HandleWithoutBodyLimit("POST /test/body/unlimited", read)
		for _, tc := range []struct {
			path   string
			size   int
			status int
		}{
			{"/test/body", 16, 204},
			{"/test/body", int(routing.DefaultMaxBodyBytes) + 1, 413},
			{"/test/body/larger", int(routing.DefaultMaxBodyBytes) + 1, 204},
			{"/test/body/larger", 2*int(routing.DefaultMaxBodyBytes) + 1, 413},
			{"/test/body/unlimited", 2*int(routing.DefaultMaxBodyBytes) + 1, 204},
		} {
			for _, contentType := range []string{"application/json", "application/octet-stream"} {
				r := httptest.NewRequest(http.MethodPost, h.baseURL+tc.path, strings.NewReader(strings.Repeat("x", tc.size)))
				r.Header.Set("Content-Type", contentType)
				r.ContentLength = -1
				w := httptest.NewRecorder()
				h.handler.ServeHTTP(w, r)
				if w.Code != tc.status {
					t.Fatalf("%s %s: status=%d, want %d", tc.path, contentType, w.Code, tc.status)
				}
			}
		}
	})
}

func TestHTTPMultipartBodyOverride(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		const bodyLimit = 2 * routing.DefaultMaxBodyBytes
		var received string
		routing.NewRoutes(h.app.Web).HandleWithBodyLimit("POST /test/upload", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseMultipartForm(1024); err != nil {
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					http.Error(w, "Upload too large", http.StatusRequestEntityTooLarge)
					return
				}
				t.Error(err)
				http.Error(w, "Upload failed", http.StatusBadRequest)
				return
			}
			defer r.MultipartForm.RemoveAll()
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			defer file.Close()
			data, err := io.ReadAll(file)
			if err != nil {
				t.Error(err)
				return
			}
			received = string(data)
			w.WriteHeader(http.StatusNoContent)
		}), bodyLimit)

		for _, tc := range []struct {
			name   string
			size   int
			status int
		}{
			{"larger upload", int(routing.DefaultMaxBodyBytes) + 1, http.StatusNoContent},
			{"oversized upload", int(bodyLimit) + 1, http.StatusRequestEntityTooLarge},
		} {
			for _, unknownLength := range []bool{false, true} {
				received = ""
				payload := strings.Repeat("x", tc.size)
				var body bytes.Buffer
				writer := multipart.NewWriter(&body)
				file, err := writer.CreateFormFile("file", "test.txt")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := io.WriteString(file, payload); err != nil {
					t.Fatal(err)
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				r := httptest.NewRequest(http.MethodPost, h.baseURL+"/test/upload", &body)
				r.Header.Set("Content-Type", writer.FormDataContentType())
				if unknownLength {
					r.ContentLength = -1
				}
				w := httptest.NewRecorder()
				h.handler.ServeHTTP(w, r)
				if w.Code != tc.status {
					t.Fatalf("%s unknown length=%t: status=%d body=%s", tc.name, unknownLength, w.Code, w.Body.String())
				}
				if tc.status == http.StatusNoContent && received != payload {
					t.Fatal("accepted upload was truncated or unreadable")
				}
				if tc.status == http.StatusRequestEntityTooLarge && received != "" {
					t.Fatal("oversized upload reached file processing")
				}
			}
		}
	})
}

func TestHTTPFormLimits(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		u := h.seedUser(t, "limits@example.invalid")
		requestNumber := 0
		valid := url.Values{
			"email": {u.Email}, "name": {"Test person"}, "message": {"Hello"},
			"password": {"TestPassword1!"}, "passwordMatch": {"TestPassword1!"},
			"oldPassword": {"TestPassword1!"}, "newPassword": {"NewPassword1!"}, "newPasswordMatch": {"NewPassword1!"},
		}.Encode()
		for _, path := range []string{
			"/contact", "/auth/login/email", "/auth/register/email", "/auth/reset/request/email",
			"/auth/reset/email", "/auth/verify", "/auth/change/email",
		} {
			if path == "/auth/change/email" {
				h.establishSession(t, u)
			}
			for _, htmx := range []bool{false, true} {
				for _, tc := range []struct {
					name, body, query, contentType string
					status                         int
				}{
					{name: "oversized body", body: valid + "&extra=" + strings.Repeat("x", int(routing.DefaultMaxBodyBytes)), status: 413},
					{name: "oversized field", body: valid + "&extra=" + strings.Repeat("x", form.MaxFieldBytes+1), status: 413},
					{name: "oversized query field", body: valid, query: "?extra=" + strings.Repeat("x", form.MaxFieldBytes+1), status: 413},
					{name: "malformed body", body: valid + "&extra=private%ZZ", status: 400},
					{name: "malformed query", body: valid, query: "?extra=private%ZZ", status: 400},
					{name: "multipart", body: valid, contentType: "multipart/form-data; boundary=test", status: 415},
				} {
					r := httptest.NewRequest(http.MethodPost, h.baseURL+path+tc.query, strings.NewReader(tc.body))
					// Isolate parser cases from the independent per-client rate limit.
					requestNumber++
					r.RemoteAddr = "192.0.2." + strconv.Itoa(requestNumber) + ":1000"
					r.Header.Set("Origin", h.baseURL)
					r.Header.Set("Sec-Fetch-Site", "same-origin")
					r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					if tc.contentType != "" {
						r.Header.Set("Content-Type", tc.contentType)
					}
					if htmx {
						r.Header.Set("HX-Request", "true")
					}
					for _, cookie := range h.cookies.Cookies(r.URL) {
						r.AddCookie(cookie)
					}
					// Exercise the streaming limit instead of Content-Length rejection.
					r.ContentLength = -1
					w := httptest.NewRecorder()
					h.handler.ServeHTTP(w, r)
					if w.Code != tc.status || strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "TestPassword1!") {
						t.Fatalf("%s %s HTMX=%t: status=%d body=%q", path, tc.name, htmx, w.Code, w.Body.String())
					}
					if len(w.Result().Cookies()) != 0 || w.Header().Get("Location") != "" || w.Header().Get("HX-Redirect") != "" {
						t.Fatalf("%s changed cookies or redirected after rejecting input", path)
					}
				}
			}
		}
		h.assertRowCount(t, "user", 1)
		h.assertRowCount(t, "usertoken", 0)
		h.assertRowCount(t, "hyperserver_contact_submission", 0)
		stored, err := user.GetUserByID(h.app.Database, u.ID)
		if err != nil || stored.Password != u.Password || stored.SessionVersion != u.SessionVersion {
			t.Fatal("rejected form changed account credentials")
		}
		if len(h.mail.snapshot()) != 0 || strings.Contains(h.logs.String(), "private%ZZ") || strings.Contains(h.logs.String(), "TestPassword1!") {
			t.Fatal("rejected input caused mail delivery or exposed input in logs")
		}
	})
}

func TestHTTPContactFieldBoundary(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		message := strings.Repeat("x", form.MaxFieldBytes)
		for _, htmx := range []bool{false, true} {
			w := h.request(http.MethodPost, "/contact", url.Values{
				"name": {"Test person"}, "email": {"person@example.invalid"}, "message": {message},
			}, htmx)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "Your message has been submitted.") {
				t.Fatalf("boundary submission: status=%d body=%s", w.Code, w.Body.String())
			}
		}
		var stored []string
		if err := h.app.Database.Select(&stored, "SELECT message FROM hyperserver_contact_submission"); err != nil {
			t.Fatal(err)
		}
		if len(stored) != 2 || stored[0] != message || stored[1] != message {
			t.Fatal("accepted contact messages were truncated or not saved")
		}
	})
}
