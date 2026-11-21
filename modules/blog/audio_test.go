package blog

import (
	"bytes"
	"context"
	"encoding/json"
	"html"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

func audioJSON(t *testing.T, audio postAudio) string {
	t.Helper()
	data, err := json.Marshal(audio)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func testAudio(t *testing.T, h *harness) postAudio {
	t.Helper()
	data, err := os.ReadFile("testdata/audio.mp3")
	if err != nil {
		t.Fatal(err)
	}
	file := testDownload(t, h, "Review recording.mp3", string(data))
	return postAudio{ID: file.ID, Size: file.Size}
}

func TestMP3Validation(t *testing.T) {
	data, err := os.ReadFile("testdata/audio.mp3")
	if err != nil {
		t.Fatal(err)
	}
	// The fixture is a generated three-second tone, not a third-party recording.
	if !validMP3(data) {
		t.Fatal("valid tagged MP3 rejected")
	}
	tagSize := int(data[9]) | int(data[8])<<7 | int(data[7])<<14 | int(data[6])<<21
	untagged := data[10+tagSize:]
	if !validMP3(untagged) {
		t.Fatal("valid untagged MP3 rejected")
	}
	for _, invalid := range [][]byte{nil, []byte("ID3"), data[:10], data[:50], []byte("renamed text"), append([]byte("ID3\x04\x00\x00\x7f\x7f\x7f\x7f"), untagged...), append([]byte("ID3\x04\x00\x00\x80\x00\x00\x00"), untagged...), append([]byte("<html>"), data...)} {
		if _, err := validateDownload(invalid, "invalid.mp3"); err == nil {
			t.Fatal("invalid MP3 accepted")
		}
	}
	// Check the header combinations used by standard CBR and VBR MP3 encoders.
	for _, version := range []byte{0, 2, 3} {
		for _, sample := range []byte{0, 1, 2} {
			for _, rate := range []byte{1, 9, 14} {
				header := []byte{255, 0xe3 | version<<3, rate<<4 | sample<<2, 0}
				frame := make([]byte, mp3FrameSize(header))
				copy(frame, header)
				if !validMP3(append(slices.Clone(frame), frame...)) {
					t.Fatalf("version %d, sample %d, rate %d rejected", version, sample, rate)
				}
			}
		}
	}
	for length := 0; length < len(data); length += 17 {
		validMP3(data[:length]) // Truncated input must never panic.
	}
}

func TestAudioComponent(t *testing.T) {
	audio := postAudio{ID: strings.Repeat("a", 64) + ".mp3", Size: 1024, Title: `<script>A title</script>`, Description: "Description <markup> & quotes.", Transcript: "**Spoken words.**\n\n<script>alert(1)</script>\n\n[Unsafe](javascript:alert(1))"}
	for _, hidden := range []bool{false, true} {
		audio.HideDownload = hidden
		source, err := joinPostComponents("Body", componentInput{"audio": audioJSON(t, audio), "video": videoJSON(t, postVideo{URL: "https://youtu.be/M7lc1UVf-VE"}), "componentOrder": `["audio","video"]`})
		if err != nil {
			t.Fatal(err)
		}
		body, components, err := splitPostComponents(source)
		if err != nil || body != "Body" || components.Audio() == nil || *components.Audio() != audio || !slices.Equal(components.Order(), []string{"audio", "video"}) {
			t.Fatal("audio did not round trip", components, err)
		}
		if got := newComponentEditorForm(source, nil).fields["audio"]; got != audioJSON(t, audio) {
			t.Fatal("editor lost audio details", got)
		}
		rendered, err := markdown(source)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`<audio controls preload="none"`, `?preview=1`, html.EscapeString(audio.Title), html.EscapeString(audio.Description), `<details class="audio-transcript">`, `<strong>Spoken words.</strong>`} {
			if !strings.Contains(string(rendered), want) {
				t.Fatal("audio rendering missing", want, rendered)
			}
		}
		for _, unsafe := range []string{"<script>", `href="javascript:`, " autoplay"} {
			if strings.Contains(string(rendered), unsafe) {
				t.Fatal("unsafe or automatic playback", rendered)
			}
		}
		if strings.Contains(string(rendered), ` download>`) == hidden {
			t.Fatal("incorrect download setting", rendered)
		}
		refs, err := contentMediaReferences(source, "/post/s/audio", "")
		if err != nil || !slices.Equal(refs, []string{"/files/" + audio.ID}) {
			t.Fatal("playback must retain audio with the download link hidden", refs, err)
		}
		feed, err := markdownAt(source, &url.URL{Scheme: "https", Host: "blog.example", Path: "/post/s/audio"})
		if err != nil || strings.Contains(string(feed), "<audio") || !strings.Contains(string(feed), `href="https://blog.example/files/`+audio.ID+`?preview=1"`) {
			t.Fatal("feed needs an absolute listening link", feed, err)
		}
		search := searchableWriting(source)
		if !strings.Contains(search, audio.Title) || !strings.Contains(search, audio.Description) || !strings.Contains(search, "Spoken words.") || strings.Contains(search, audio.ID) {
			t.Fatal("incorrect audio search text", search)
		}
		missing, err := markMissingMedia(rendered, map[string]bool{"/files/" + audio.ID: true})
		if err != nil || !strings.Contains(string(missing), "Audio file deleted") || strings.Contains(string(missing), `<audio controls`) {
			t.Fatal("missing audio needs a useful history placeholder", missing, err)
		}
	}
	for _, invalid := range []string{"{}", "null", `{"id":"https://example.test/remote.mp3","size":1}`, `{"id":"` + audio.ID + `","size":1,"title":false}`, strings.Replace(audioJSON(t, audio), ".mp3", ".pdf", 1), audioJSON(t, postAudio{ID: audio.ID, Size: maxDownloadBytes + 1}), audioJSON(t, postAudio{ID: audio.ID, Size: 1, Transcript: strings.Repeat("x", 20001)})} {
		if _, err := joinPostComponents("Body", componentInput{"audio": invalid}); err == nil {
			t.Fatal("invalid audio accepted", invalid)
		}
	}
	// Audio cannot be smuggled into another component's payload.
	envelope := `{"version":2,"components":[{"type":"video","video":{"url":"https://youtu.be/M7lc1UVf-VE"},"audio":` + audioJSON(t, audio) + `}]}`
	if _, err := parseComponents(envelope); err == nil {
		t.Fatal("mixed payload accepted")
	}
}

func TestAudioFormsHistoryAndAccess(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "htmx"}[hx], func(t *testing.T) {
			h := newHarness(t)
			h.register(t)
			h.verify(t)
			h.login(t)
			audio := testAudio(t, h)
			audio.HideDownload, audio.Transcript = true, "The private recording transcript."
			path := "/files/" + audio.ID + "?preview=1"
			public := func(method string, headers map[string]string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(method, "https://blog.example"+path, nil)
				for key, value := range headers {
					r.Header.Set(key, value)
				}
				w := httptest.NewRecorder()
				h.app.Handler.ServeHTTP(w, r)
				return w
			}
			private := func() {
				t.Helper()
				for _, method := range []string{"GET", "HEAD"} {
					for _, headers := range []map[string]string{nil, {"Range": "bytes=0-3"}, {"If-None-Match": "*"}} {
						w := public(method, headers)
						requireStatus(t, w, http.StatusNotFound)
						if w.Header().Get("Cache-Control") != "no-store" {
							t.Fatal("private audio must not be cached")
						}
					}
				}
			}
			private()
			inline := h.request("GET", path, nil, false)
			requireStatus(t, inline, http.StatusOK)
			disposition, _, err := mime.ParseMediaType(inline.Header().Get("Content-Disposition"))
			if err != nil || disposition != "inline" || inline.Header().Get("Content-Type") != "audio/mpeg" {
				t.Fatal("audio response must support native playback", inline.Header(), err)
			}
			fields := url.Values{"title": {""}, "slug": {"audio-review"}, "audio": {audioJSON(t, audio)}, "componentOrder": {`["audio"]`}}
			failure := h.request("POST", "/post", fields, hx)
			requireStatus(t, failure, http.StatusUnprocessableEntity)
			if !hx && !strings.Contains(html.UnescapeString(failure.Body.String()), audio.Transcript) {
				t.Fatal("ordinary failure lost audio source")
			}
			fields.Set("title", "Audio review")
			for _, invalid := range []postAudio{{ID: audio.ID, Size: audio.Size + 1}, {ID: strings.Repeat("b", 64) + ".mp3", Size: audio.Size}} {
				fields.Set("audio", audioJSON(t, invalid))
				failure = h.request("POST", "/post", fields, hx)
				requireStatus(t, failure, http.StatusUnprocessableEntity)
				if !strings.Contains(failure.Body.String(), `data-error-fields="audio"`) {
					t.Fatal("file validation must identify audio")
				}
			}
			fields.Set("audio", audioJSON(t, audio))
			status := http.StatusSeeOther
			if hx {
				status = http.StatusOK
			}
			requireStatus(t, h.request("POST", "/post", fields, hx), status)
			post, err := h.blog.posts.GetBySlug(t.Context(), "audio-review", true)
			if err != nil {
				t.Fatal(err)
			}
			versions, err := h.blog.history.ListRevisions(t.Context(), "post", post.ID)
			if err != nil || len(versions) != 1 {
				t.Fatal("missing audio revision", err)
			}
			original, err := h.blog.history.GetRevision(t.Context(), "post", post.ID, versions[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			private()
			post.Draft, post.Published = false, time.Now().Add(time.Hour)
			post, err = savePostFixture(h.blog.posts, t.Context(), post)
			if err != nil {
				t.Fatal(err)
			}
			private()
			post, err = setPostPublication(h.blog.posts, t.Context(), post.ID, false)
			if err != nil {
				t.Fatal(err)
			}
			post, err = setPostPublication(h.blog.posts, t.Context(), post.ID, true)
			if err != nil {
				t.Fatal(err)
			}
			response := public("GET", nil)
			requireStatus(t, response, http.StatusOK)
			requireStatus(t, public("HEAD", nil), http.StatusOK)
			requireStatus(t, public("GET", map[string]string{"If-None-Match": response.Header().Get("ETag")}), http.StatusNotModified)
			ranged := public("GET", map[string]string{"Range": "bytes=0-3"})
			requireStatus(t, ranged, http.StatusPartialContent)
			if !bytes.Equal(ranged.Body.Bytes(), response.Body.Bytes()[:4]) {
				t.Fatal("seeking returned incorrect bytes")
			}
			reader := h.request("GET", "/post/s/audio-review", nil, false)
			if !strings.Contains(reader.Header().Get("Content-Security-Policy"), "media-src 'self'") {
				t.Fatal("reader must allow local playback")
			}
			post.Markdown = "Audio removed"
			post, err = savePostFixture(h.blog.posts, t.Context(), post)
			if err != nil {
				t.Fatal(err)
			}
			private()
			usage, err := h.blog.mediaReferences.ReferencedMedia(t.Context(), audio.ID)
			if err != nil || usage[audio.ID].Current || len(usage[audio.ID].History) == 0 {
				t.Fatal("history must retain audio without making it public", usage, err)
			}
			post, err = setPostPublication(h.blog.posts, t.Context(), post.ID, false)
			if err != nil {
				t.Fatal(err)
			}
			if err := h.blog.restoreRevisionContent(adminWritingContext(t.Context()), "post", post.ID, post.Version, original); err != nil {
				t.Fatal(err)
			}
			restored, err := h.blog.posts.GetByID(t.Context(), post.ID, true)
			if err != nil || restored.Markdown != original.Markdown {
				t.Fatal("audio restoration lost settings", err)
			}
			private()
		})
	}
}

func TestAudioOwnership(t *testing.T) {
	h := usersHarness(t)
	audio := testAudio(t, h)
	audio.HideDownload = true
	poster := registeredPoster(t, h)
	ctx := context.WithValue(t.Context(), user.UserContextKey, poster)
	path := "/files/" + audio.ID + "?preview=1"
	requireStatus(t, h.request("GET", path, nil, false), http.StatusNotFound)
	if strings.Contains(h.request("GET", "/post/files", nil, false).Body.String(), audio.ID) {
		t.Fatal("another author's private MP3 appeared in the library")
	}
	source, err := joinPostComponents("", componentInput{"audio": audioJSON(t, audio)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.blog.writePost(ctx, Post{Title: "Private MP3", Markdown: source, Draft: true}); err == nil {
		t.Fatal("poster attached another author's private MP3")
	}
	post, err := h.blog.persistPost(t.Context(), Post{Title: "Authorized MP3", AuthorID: poster.ID, Markdown: source, Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, h.request("GET", path, nil, false), http.StatusOK)
	post.Markdown = "Audio removed"
	if _, err := h.blog.persistPost(t.Context(), post); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, h.request("GET", path, nil, false), http.StatusOK)
	if !strings.Contains(h.request("GET", "/post/files", nil, false).Body.String(), audio.ID) {
		t.Fatal("poster lost access to an MP3 retained in their own history")
	}
}
