package blog

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSocialPostPreview(t *testing.T) {
	for _, hx := range []bool{false, true} {
		h := registeredBrowserHarness(t)
		input := postSocialPost{URL: "https://x.com/user/status/123", Description: "Context <script> & text", ImageID: strings.Repeat("b", 64) + ".png"}
		fields := url.Values{"socialPost": {componentJSON(input)}}
		requireStatus(t, h.request("POST", "/post/social-preview", fields, hx), http.StatusForbidden)
		h.login(t)
		cases := append(socialPostCases(), additionalSocialPostCases()...)
		cases = append(cases, struct {
			provider socialProvider
			url      string
		}{xProvider(), input.URL})
		for _, sample := range cases {
			input.URL = sample.url
			fields.Set("socialPost", componentJSON(input))
			response := h.request("POST", "/post/social-preview", fields, hx)
			requireStatus(t, response, http.StatusOK)
			source, err := joinPostComponents("", componentInput{"socialPost": componentJSON(input), "componentOrder": `["social-post"]`})
			if err != nil {
				t.Fatal(err)
			}
			published, err := markdown(source)
			if err != nil || response.Body.String() != string(published) {
				t.Fatal("the editor must use the published renderer", response.Body.String(), err)
			}
			if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Content-Type") != "text/html; charset=utf-8" || strings.Contains(response.Body.String(), "<script>") {
				t.Fatal("preview output must remain safe and private", response.Header(), response.Body.String())
			}
		}
		posts, err := h.blog.posts.ListMetadata(t.Context())
		if err != nil || len(posts) != 0 {
			t.Fatal("previewing must not create a post", posts, err)
		}
		for _, invalid := range []string{"", "null", "{}", `{"url":"javascript:alert(1)"}`, `{"url":"https://example.test/post/123"}`, `{"url":"https://localhost/@user/123"}`, `{"url":"https://x.com/user/status/123","imageID":"../../private"}`} {
			fields.Set("socialPost", invalid)
			requireStatus(t, h.request("POST", "/post/social-preview", fields, hx), http.StatusUnprocessableEntity)
		}
		fields.Set("socialPost", strings.Repeat("x", 8*1024+1))
		requireStatus(t, h.request("POST", "/post/social-preview", fields, hx), http.StatusRequestEntityTooLarge)
		fields.Set("socialPost", strings.Repeat("x", 32*1024))
		requireStatus(t, h.request("POST", "/post/social-preview", fields, hx), http.StatusRequestEntityTooLarge)
		fields.Set("socialPost", componentJSON(input))
		request := httptest.NewRequest("POST", "https://blog.example/post/social-preview", strings.NewReader(fields.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Origin", "https://attacker.test")
		for _, cookie := range h.cookies {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		h.app.Handler.ServeHTTP(response, request)
		requireStatus(t, response, http.StatusForbidden)
	}
}

func TestPosterSocialPostPreview(t *testing.T) {
	h := posterUploadBrowserHarness(t)
	h.login(t)
	fields := url.Values{"socialPost": {`{"url":"https://x.com/user/status/123"}`}}
	requireStatus(t, h.request("POST", "/post/social-preview", fields, false), http.StatusOK)
}
