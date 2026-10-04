package form

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/routing"
)

// Exercise the parser under the same default body protection as application routes.
func parseRequest(next http.Handler) http.Handler {
	return http.MaxBytesHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := Parse(r); err != nil {
			http.Error(w, err.Error(), err.StatusCode)
			return
		}
		next.ServeHTTP(w, r)
	}), routing.DefaultMaxBodyBytes)
}

func TestParseLimits(t *testing.T) {
	// Four fields fit the exact body limit without exceeding the field limit.
	exactBody := "a=" + strings.Repeat("a", MaxFieldBytes) + "&b=" + strings.Repeat("b", MaxFieldBytes) + "&c=" + strings.Repeat("c", MaxFieldBytes) + "&d=" + strings.Repeat("d", MaxFieldBytes-11)
	for _, tc := range []struct {
		name, body, query, contentType string
		status                         int
	}{
		{name: "empty", status: 204},
		{name: "valid", body: "name=Test+person&message=caf%C3%A9", status: 204},
		{name: "field boundary", body: "x=" + strings.Repeat("a", MaxFieldBytes), status: 204},
		{name: "decoded field boundary", body: "x=" + strings.Repeat("%41", MaxFieldBytes), status: 204},
		{name: "field overflow", body: "x=" + strings.Repeat("a", MaxFieldBytes+1), status: 413},
		{name: "field name overflow", body: strings.Repeat("n", MaxFieldBytes+1) + "=x", status: 413},
		{name: "second value overflow", body: "x=ok&x=" + strings.Repeat("a", MaxFieldBytes+1), status: 413},
		{name: "UTF-8 byte limit", body: "x=" + strings.Repeat("é", MaxFieldBytes/2+1), status: 413},
		{name: "body boundary", body: exactBody, status: 204},
		{name: "body overflow", body: exactBody + "a", status: 413},
		{name: "bad body", body: "x=private%ZZ", status: 400},
		{name: "bad query", body: "x=ok", query: "x=private%ZZ", status: 400},
		{name: "query field overflow", query: "x=" + strings.Repeat("a", MaxFieldBytes+1), status: 413},
		{name: "query overflow", query: strings.Repeat("x", MaxQueryBytes+1), status: 413},
		{name: "body cannot hide query field", body: "x=ok", query: "x=" + strings.Repeat("a", MaxFieldBytes+1), status: 413},
		{name: "charset", body: "x=ok", contentType: "application/x-www-form-urlencoded; charset=UTF-8", status: 204},
		{name: "bad content type", body: "x=ok", contentType: "application/x-www-form-urlencoded; broken", status: 400},
		{name: "multipart", body: "private", contentType: "multipart/form-data; boundary=test", status: 415},
		{name: "JSON", body: "private", contentType: "application/json", status: 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, unknownLength := range []bool{false, true} {
				r := httptest.NewRequest(http.MethodPost, "/?"+tc.query, strings.NewReader(tc.body))
				contentType := tc.contentType
				if contentType == "" {
					contentType = "application/x-www-form-urlencoded"
				}
				r.Header.Set("Content-Type", contentType)
				if unknownLength {
					r.ContentLength = -1
				}
				called := false
				w := httptest.NewRecorder()
				parseRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
					want, _ := url.ParseQuery(tc.body)
					for name := range want {
						if r.PostForm.Get(name) != want.Get(name) {
							t.Errorf("field %q changed during parsing", name)
						}
					}
					w.WriteHeader(http.StatusNoContent)
				})).ServeHTTP(w, r)
				if w.Code != tc.status || called != (tc.status == 204) {
					t.Fatalf("unknown length=%t: status=%d called=%t, want %d", unknownLength, w.Code, called, tc.status)
				}
				if tc.status != 204 && (strings.Contains(w.Body.String(), "private") || w.Header().Get("Content-Type") != "text/plain; charset=utf-8") {
					t.Fatal("rejection exposed input or was not plain text")
				}
			}
		})
	}
}

type failingFormBody struct{}

func (failingFormBody) Read([]byte) (int, error) { return 0, errors.New("private reader error") }
func (failingFormBody) Close() error             { return nil }

func TestParseReadFailure(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Body = failingFormBody{}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	parseRequest(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("handler called") })).ServeHTTP(w, r)
	if w.Code != 400 || strings.Contains(w.Body.String(), "private") {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestParseMissingContentType(t *testing.T) {
	for _, body := range []string{"", "x=private"} {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		w := httptest.NewRecorder()
		parseRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, r)
		want := 204
		if body != "" {
			want = 415
		}
		if w.Code != want {
			t.Fatalf("status=%d, want %d", w.Code, want)
		}
	}
}

func TestParseStopsReading(t *testing.T) {
	body := strings.NewReader(strings.Repeat("x", 2*int(routing.DefaultMaxBodyBytes)))
	r := httptest.NewRequest(http.MethodPost, "/", io.NopCloser(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	parseRequest(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("handler called") })).ServeHTTP(w, r)
	if w.Code != 413 || body.Len() < int(routing.DefaultMaxBodyBytes)-1 {
		t.Fatalf("status=%d unread=%d", w.Code, body.Len())
	}
}

type bindingProbe struct {
	Form
	bound bool
}

func (f *bindingProbe) Bind(*http.Request) error { f.bound = true; return nil }

func TestParseAndValidateDefaultsAndOverrides(t *testing.T) {
	for _, tc := range []struct {
		name      string
		options   ParseOptions
		status    int
		bodyLimit int64
		size      int
	}{
		{name: "default", status: 413},
		{name: "larger", options: ParseOptions{MaxFieldBytes: 2 * MaxFieldBytes}, status: 204},
		{name: "larger boundary", options: ParseOptions{MaxFieldBytes: 2 * MaxFieldBytes}, size: 2 * MaxFieldBytes, status: 204},
		{name: "larger overflow", options: ParseOptions{MaxFieldBytes: 2 * MaxFieldBytes}, size: 2*MaxFieldBytes + 1, status: 413},
		{name: "smaller boundary", options: ParseOptions{MaxFieldBytes: 5}, size: 5, status: 204},
		{name: "smaller overflow", options: ParseOptions{MaxFieldBytes: 5}, size: 6, status: 413},
		{name: "disabled", options: ParseOptions{UnlimitedFields: true}, status: 204},
		{name: "negative is invalid", options: ParseOptions{MaxFieldBytes: -1}, status: 500},
		{name: "larger body keeps field limit", bodyLimit: 2 * routing.DefaultMaxBodyBytes, status: 413},
		{name: "disabled fields keep body limit", options: ParseOptions{UnlimitedFields: true}, size: int(routing.DefaultMaxBodyBytes), status: 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := new(bindingProbe)
			h := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				message, err := ParseAndValidateWithOptions(r, f, tc.options)
				if err != nil {
					// Parsing failures must not try to render or retain submitted values.
					HandleFormError(w, r, nil, f, message, err)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			bodyLimit := tc.bodyLimit
			if bodyLimit == 0 {
				bodyLimit = routing.DefaultMaxBodyBytes
			}
			size := tc.size
			if size == 0 {
				size = MaxFieldBytes + 1
			}
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x="+strings.Repeat("a", size)))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			http.MaxBytesHandler(h, bodyLimit).ServeHTTP(w, r)
			if w.Code != tc.status || f.bound != (tc.status == 204) || f.IsValidated() != f.bound {
				t.Fatalf("status=%d bound=%t validated=%t", w.Code, f.bound, f.IsValidated())
			}
		})
	}
}
