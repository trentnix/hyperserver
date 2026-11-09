package blog

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// profileRevisionForm reads the actual form, including the current concurrency token.
func profileRevisionForm(t *testing.T, body string) url.Values {
	t.Helper()
	values := url.Values{}
	for _, name := range []string{"version", "name", "imageURL", "faviconURL", "backgroundURL"} {
		match := regexp.MustCompile(`name="` + name + `" value="([^"]*)"`).FindStringSubmatch(body)
		if len(match) != 2 {
			t.Fatal("missing profile input", name)
		}
		values.Set(name, html.UnescapeString(match[1]))
	}
	match := regexp.MustCompile(`(?s)<textarea[^>]*name="bio"[^>]*>(.*?)</textarea>`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatal("missing profile bio")
	}
	values.Set("bio", html.UnescapeString(match[1]))
	return values
}

// TestProfileHistoryStorage checks snapshots, stale writes, rollback, and baseline upgrades.
func TestProfileHistoryStorage(t *testing.T) {
	h := newHarness(t)
	s := h.blog.profileStore.(*sqliteRepository)
	original, err := s.GetProfile(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	versions, err := s.ListProfileRevisions(t.Context())
	if err != nil || len(versions) != 1 {
		t.Fatal(versions, err)
	}
	if err := s.SaveProfile(t.Context(), original); !errors.Is(err, errProfileUnchanged) {
		t.Fatal(err)
	}
	if current, err := s.GetProfile(t.Context()); err != nil || current != original {
		t.Fatal("unchanged save replaced the profile", current, err)
	}
	unchanged, _ := s.ListProfileRevisions(t.Context())
	if !reflect.DeepEqual(versions, unchanged) {
		t.Fatal("unchanged content created history")
	}
	p, _ := s.GetProfile(t.Context())
	p.Name, p.Bio, p.ImageURL, p.FaviconURL, p.BackgroundURL = "Changed", "New bio", "/portrait.png", "/icon.png", "/background.png"
	if err := s.SaveProfile(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveProfile(t.Context(), p); !errors.Is(err, errEditConflict) {
		t.Fatal("stale write accepted", err)
	}
	versions, err = s.ListProfileRevisions(t.Context())
	if err != nil || len(versions) != 2 {
		t.Fatal(versions, err)
	}
	snapshot, err := s.GetProfileRevision(t.Context(), versions[0].ID)
	p.Version = 0 // Snapshot IDs are not edit tokens.
	if err != nil || snapshot.Profile != p {
		t.Fatal(snapshot, p, err)
	}
	if _, err := s.GetProfileRevision(t.Context(), 999999); !errors.Is(err, errNotFound) {
		t.Fatal(err)
	}

	current, _ := s.GetProfile(t.Context())
	if _, err := s.db.ExecContext(t.Context(), `CREATE TRIGGER fail_profile_history BEFORE INSERT ON blog_profile BEGIN SELECT RAISE(ABORT, 'history failed'); END`); err != nil {
		t.Fatal(err)
	}
	p = current
	p.Name = "Must roll back"
	if err := s.SaveProfile(t.Context(), p); err == nil {
		t.Fatal("snapshot failure reported success")
	}
	got, err := s.GetProfile(t.Context())
	if err != nil || got != current {
		t.Fatal("failed snapshot changed current profile", got, err)
	}
	if _, err := s.db.ExecContext(t.Context(), "DROP TRIGGER fail_profile_history"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.ListProfileRevisions(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.GetProfileRevision(ctx, versions[0].ID); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	cfg := *h.app.Config
	if err := h.app.Shutdown(); err != nil {
		t.Fatal(err)
	}
	restarted := startHarness(t, cfg)
	after, err := restarted.blog.profileStore.ListProfileRevisions(t.Context())
	if err != nil || !reflect.DeepEqual(after, versions) {
		t.Fatal("restart changed history", after, err)
	}
}

// TestProfileHistoryHTTP verifies permissions, preview fragments, restoration, and conflict handling.
func TestProfileHistoryHTTP(t *testing.T) {
	for _, hx := range []bool{false, true} {
		h := newHarness(t)
		for _, path := range []string{"/profile/history", "/profile?revision=1"} {
			requireStatus(t, h.request("GET", path, nil, hx), http.StatusForbidden)
		}
		h.register(t)
		h.login(t)
		requireStatus(t, h.request("GET", "/profile/history", nil, hx), http.StatusForbidden)
		h.verify(t)
		if strings.Contains(h.request("GET", "/profile", nil, hx).Body.String(), `href="/profile/history"`) {
			t.Fatal("one snapshot offered history")
		}
		original := h.blog.profile.Current()
		versions, _ := h.blog.profileStore.ListProfileRevisions(t.Context())
		old := strconv.FormatInt(versions[0].ID, 10)
		p := original
		p.Name, p.Bio = "Current <name>", "Current <bio>"
		if err := h.blog.persistProfile(t.Context(), p); err != nil {
			t.Fatal(err)
		}
		current := h.blog.profile.Current()
		historyURL := "/profile/history?revision=" + old
		editorURL := "/profile?revision=" + old
		for _, path := range []string{"/profile/history?revision=invalid", "/profile/history?revision=0", "/profile/history?revision=999999", "/profile?revision=-1", "/profile?revision=999999"} {
			requireStatus(t, h.request("GET", path, nil, hx), http.StatusNotFound)
		}
		w := h.request("GET", historyURL, nil, hx)
		requireStatus(t, w, http.StatusOK)
		if !strings.Contains(w.Body.String(), `href="`+editorURL+`"`) || !strings.Contains(w.Body.String(), `class="revision-list"`) || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("missing private history UI")
		}
		for _, restore := range []bool{false, true} {
			r := httptest.NewRequest("GET", historyURL, nil)
			r.Header.Set("HX-Request", "true")
			r.Header.Set("HX-Target", "div#revision-preview")
			r.Header.Set("HX-History-Restore-Request", strconv.FormatBool(restore))
			for _, cookie := range h.cookies {
				r.AddCookie(cookie)
			}
			w = httptest.NewRecorder()
			h.app.Handler.ServeHTTP(w, r)
			requireStatus(t, w, http.StatusOK)
			if strings.Contains(w.Body.String(), `class="revision-list"`) != restore || !strings.Contains(w.Header().Get("Vary"), "HX-Target") {
				t.Fatal("wrong fragment/history restore")
			}
			if !restore && w.Header().Get("HX-Replace-Url") != historyURL {
				t.Fatal("preview did not update history URL")
			}
		}
		editor := h.request("GET", editorURL, nil, hx)
		requireStatus(t, editor, http.StatusOK)
		values := profileRevisionForm(t, editor.Body.String())
		if values.Get("name") != original.Name || values.Get("version") != strconv.FormatInt(current.Version, 10) || h.blog.profile.Current() != current {
			t.Fatal("loading history changed current profile or edit token")
		}
		values.Set("name", "")
		w = h.request("POST", editorURL, values, hx)
		requireStatus(t, w, http.StatusUnprocessableEntity)
		if !hx && !strings.Contains(w.Body.String(), `action="`+editorURL+`"`) {
			t.Fatal("validation lost history context")
		}
		values.Set("name", original.Name)
		w = h.request("POST", editorURL, values, hx)
		want := http.StatusSeeOther
		if hx {
			want = http.StatusOK
		}
		requireStatus(t, w, want)
		original.Version = current.Version + 1
		if h.blog.profile.Current() != original {
			t.Fatal("restoration did not save all fields")
		}
		versions, _ = h.blog.profileStore.ListProfileRevisions(t.Context())
		if len(versions) != 3 {
			t.Fatal("restore did not append history")
		}
		values.Set("name", "Stale edit")
		w = h.request("POST", editorURL, values, hx)
		requireStatus(t, w, http.StatusConflict)
		if !strings.Contains(w.Body.String(), editConflictMessage) || h.blog.profile.Current() != original {
			t.Fatal("stale editor overwrote profile")
		}
		for _, token := range []string{"", "invalid", "-1", "9223372036854775808"} {
			values.Set("version", token)
			requireStatus(t, h.request("POST", "/profile", values, hx), http.StatusBadRequest)
		}
	}
}

// TestRestoreProfile checks direct restores, authorization, stale tokens, and storage failures.
func TestRestoreProfile(t *testing.T) {
	for _, hx := range []bool{false, true} {
		h := newHarness(t)
		original := h.blog.profile.Current()
		versions, _ := h.blog.profileStore.ListProfileRevisions(t.Context())
		old := versions[0]
		path := "/profile/restore?revision=" + strconv.FormatInt(old.ID, 10)
		values := url.Values{"version": {strconv.FormatInt(original.Version, 10)}}
		requireStatus(t, h.request("POST", path, values, hx), http.StatusForbidden)
		h.register(t)
		h.login(t)
		requireStatus(t, h.request("POST", path, values, hx), http.StatusForbidden)
		h.verify(t)

		p := original
		p.Name, p.Bio, p.ImageURL, p.FaviconURL, p.BackgroundURL = "Changed", "Changed bio", "", "", ""
		if err := h.blog.persistProfile(t.Context(), p); err != nil {
			t.Fatal(err)
		}
		current := h.blog.profile.Current()
		requireStatus(t, h.request("POST", path, values, hx), http.StatusConflict)
		for _, token := range []string{"", "invalid", "-1", "0", "9223372036854775808"} {
			values.Set("version", token)
			requireStatus(t, h.request("POST", path, values, hx), http.StatusBadRequest)
		}
		values.Set("version", strconv.FormatInt(current.Version, 10))
		for _, revision := range []string{"", "invalid", "0", "-1", "999999"} {
			requireStatus(t, h.request("POST", "/profile/restore?revision="+revision, values, hx), http.StatusNotFound)
		}
		store := h.blog.profileStore
		h.blog.profileStore = failingProfileStore{store}
		requireStatus(t, h.request("POST", path, values, hx), http.StatusInternalServerError)
		h.blog.profileStore = unavailableProfileStore{store}
		requireStatus(t, h.request("POST", path, values, hx), http.StatusInternalServerError)
		h.blog.profileStore = store
		if h.blog.profile.Current() != current {
			t.Fatal("failed restore changed current profile")
		}
		// Submitted content must not override the stored snapshot.
		values.Set("name", "Not the saved name")
		w := h.request("POST", path, values, hx)
		if hx {
			requireStatus(t, w, http.StatusOK)
			if w.Header().Get("HX-Redirect") != "/profile/history" {
				t.Fatal("restore did not reload the shared profile")
			}
		} else {
			requireStatus(t, w, http.StatusSeeOther)
			if w.Header().Get("Location") != "/profile/history" {
				t.Fatal("restore did not return to history")
			}
		}
		original.Version = current.Version + 1
		if h.blog.profile.Current() != original {
			t.Fatal("restore lost saved fields")
		}
		versions, _ = store.ListProfileRevisions(t.Context())
		if len(versions) != 3 || versions[2] != old {
			t.Fatal("restore did not preserve history")
		}
		requireStatus(t, h.request("POST", path, values, hx), http.StatusConflict)
		// A matching historical version must not replace current content or add history.
		before := h.blog.profile.Current()
		beforeVersions := versions
		values.Set("version", strconv.FormatInt(h.blog.profile.Current().Version, 10))
		w = h.request("POST", path, values, false)
		requireStatus(t, w, http.StatusSeeOther)
		versions, _ = store.ListProfileRevisions(t.Context())
		if !reflect.DeepEqual(versions, beforeVersions) || h.blog.profile.Current() != before {
			t.Fatal("unchanged restore changed the profile or history")
		}
		w = h.request("GET", w.Header().Get("Location"), nil, false)
		requireStatus(t, w, http.StatusOK)
		if !strings.Contains(w.Body.String(), `data-revision="`+strconv.FormatInt(versions[0].ID, 10)+`" aria-current="true"`) {
			t.Fatal("history did not select the new restore")
		}
		w = h.request("GET", "/profile/history?revision="+strconv.FormatInt(old.ID, 10), nil, false)
		if !strings.Contains(w.Body.String(), "This version matches the current content.") || strings.Contains(w.Body.String(), "Restore this version</button>") {
			t.Fatal("matching history should explain why restoration is unavailable")
		}
	}
}

// TestProfileHistoryMedia covers all image roles, cleanup protection, and explicit deletion recovery.
func TestProfileHistoryMedia(t *testing.T) {
	h := newHarness(t)
	m := h.blog
	f := mediaUpload(t, m, "historical profile")
	p := m.profile.Current()
	p.ImageURL, p.FaviconURL, p.BackgroundURL = "/images/"+f.ID, "/images/"+f.ID, "/images/"+f.ID
	if err := m.persistProfile(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if err := m.removeMedia(t.Context(), f.ID, f.Version(), true); !errors.Is(err, errMediaUsed) {
		t.Fatal("current profile image was removable", err)
	}
	versions, _ := m.profileStore.ListProfileRevisions(t.Context())
	old := strconv.FormatInt(versions[0].ID, 10)
	p = m.profile.Current()
	p.ImageURL, p.FaviconURL, p.BackgroundURL = "", "", ""
	if err := m.persistProfile(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	used, err := m.mediaReferences.ReferencedImages(t.Context())
	if err != nil || used[f.ID].Current || len(used[f.ID].History) != 1 || used[f.ID].History[0].URL != "/profile/history?revision="+old {
		t.Fatal("incorrect or duplicate profile history references", used, err)
	}
	if public, err := m.imageAccess.ImageIsPublic(t.Context(), "/images/"+f.ID); err != nil || public {
		t.Fatal("history made an image public", public, err)
	}
	h.register(t)
	h.verify(t)
	h.login(t)
	w := h.request("POST", "/media/delete-unused", mediaBatchForm(f), false)
	if !strings.Contains(w.Body.String(), "Deleted: 0. Skipped: 1.") {
		t.Fatal("bulk cleanup deleted historical profile image")
	}
	w = h.request("GET", "/media/delete/"+f.ID, nil, false)
	if !strings.Contains(w.Body.String(), "/profile/history?revision="+old) || !strings.Contains(w.Body.String(), `name="deleteHistory"`) {
		t.Fatal("confirmation lacks historical profile reference")
	}
	if err := m.removeMedia(t.Context(), f.ID, f.Version(), false); err == nil {
		t.Fatal("history-only deletion required no confirmation")
	}
	if err := m.removeMedia(t.Context(), f.ID, f.Version(), true); err != nil {
		t.Fatal(err)
	}
	for _, hx := range []bool{false, true} {
		w = h.request("GET", "/profile/history?revision="+old, nil, hx)
		requireStatus(t, w, http.StatusOK)
		if strings.Count(w.Body.String(), "Image deleted") != 3 || strings.Contains(w.Body.String(), `src="/images/`+f.ID) {
			t.Fatal("history did not mark deleted images")
		}
		w = h.request("GET", "/profile?revision="+old, nil, hx)
		if !strings.Contains(w.Body.String(), "Replace or remove their references before saving.") {
			t.Fatal("restoration lacks missing-image guidance")
		}
		if strings.Contains(w.Body.String(), `src="/images/`+f.ID) || strings.Count(w.Body.String(), "data-image-missing") != 3 {
			t.Fatal("editor shows broken image previews")
		}
		values := profileRevisionForm(t, w.Body.String())
		requireStatus(t, h.request("POST", "/profile?revision="+old, values, hx), http.StatusUnprocessableEntity)
		before := m.profile.Current()
		w = h.request("POST", "/profile/restore?revision="+old, values, hx)
		requireStatus(t, w, http.StatusUnprocessableEntity)
		if m.profile.Current() != before || !strings.Contains(w.Body.String(), "no longer available") {
			t.Fatal("direct restore accepted deleted images")
		}
	}
}

// unavailableProfileStore models simultaneous save and history failures.
type unavailableProfileStore struct{ profileRepository }

func (unavailableProfileStore) SaveProfile(context.Context, Profile) error {
	return errors.New("private profile storage failure")
}

func (unavailableProfileStore) ListProfileRevisions(context.Context) ([]ProfileRevision, error) {
	return nil, errors.New("private profile storage failure")
}

func (unavailableProfileStore) GetProfileRevision(context.Context, int64) (ProfileRevision, error) {
	return ProfileRevision{}, errors.New("private profile storage failure")
}

// TestProfileHistoryFailure preserves edits even when error rendering cannot read history.
func TestProfileHistoryFailure(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	original := h.blog.profile.Current()
	h.blog.profileStore = unavailableProfileStore{h.blog.profileStore}
	for _, hx := range []bool{false, true} {
		for _, path := range []string{"/profile", "/profile/history", "/profile?revision=1"} {
			w := h.request("GET", path, nil, hx)
			requireStatus(t, w, http.StatusInternalServerError)
			if strings.Contains(w.Body.String(), "private profile storage failure") {
				t.Fatal("storage error leaked")
			}
		}
		w := h.request("POST", "/profile", profileFields("Unsaved name"), hx)
		requireStatus(t, w, http.StatusInternalServerError)
		if strings.Contains(w.Body.String(), "private profile storage failure") {
			t.Fatal("storage error leaked")
		}
		if !hx {
			values := profileRevisionForm(t, w.Body.String())
			if values.Get("name") != "Unsaved name" || values.Get("version") != "1" {
				t.Fatal("failed save lost submitted content or token")
			}
		}
	}
	if h.blog.profile.Current() != original {
		t.Fatal("failed save changed live profile")
	}
}

// TestProfileHistoryMediaRoles checks each image role independently, including original-host URLs.
func TestProfileHistoryMediaRoles(t *testing.T) {
	for _, role := range []string{"portrait", "favicon", "background"} {
		t.Run(role, func(t *testing.T) {
			h := newHarness(t)
			m := h.blog
			f := mediaUpload(t, m, role)
			p := m.profile.Current()
			ref := "https://old.example/images/" + f.ID + "?size=small#image"
			switch role {
			case "portrait":
				p.ImageURL = ref
			case "favicon":
				p.FaviconURL = ref
			case "background":
				p.BackgroundURL = ref
			}
			if err := m.persistProfile(t.Context(), p); err != nil {
				t.Fatal(err)
			}
			p = m.profile.Current()
			p.ImageURL, p.FaviconURL, p.BackgroundURL = "", "", ""
			if err := m.persistProfile(t.Context(), p); err != nil {
				t.Fatal(err)
			}
			used, err := m.mediaReferences.ReferencedImages(t.Context())
			if err != nil || len(used[f.ID].History) != 1 || used[f.ID].Current {
				t.Fatal("lost historical image reference", used, err)
			}
		})
	}
}
