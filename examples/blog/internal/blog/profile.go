package blog

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/util"
)

// Profile is the stored public identity used by shared layouts and RSS.
// Image URLs can be local absolute paths or HTTPS URLs. Empty uses neutral defaults.
type Profile struct {
	Name, Bio, ImageURL, FaviconURL, BackgroundURL string
}

// validate rejects incomplete text and unsafe image URLs before saving.
func (p Profile) validate() error {
	if strings.TrimSpace(p.Name) == "" || !utf8.ValidString(p.Name) || utf8.RuneCountInString(p.Name) > 240 {
		return errors.New("Enter a name of 1–240 characters.")
	}
	if !utf8.ValidString(p.Bio) || utf8.RuneCountInString(p.Bio) > 500 {
		return errors.New("Keep the short bio under 500 characters.")
	}
	for _, image := range []struct{ name, value string }{
		{"Portrait", p.ImageURL},
		{"Favicon", p.FaviconURL},
		{"Background", p.BackgroundURL},
	} {
		if !utf8.ValidString(image.value) || len(image.value) > 2048 {
			return errors.New(image.name + " URL must contain at most 2,048 UTF-8 bytes.")
		}
		if image.value == "" || util.SafeRedirectURL(image.value, "") == image.value {
			continue
		}
		u, err := url.Parse(image.value)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			return errors.New(image.name + " URL must be a local absolute path or HTTPS URL.")
		}
	}
	return nil
}

// PortraitURL selects the stored profile image or the bundled neutral image.
func (p Profile) PortraitURL() string {
	if p.ImageURL == "" {
		return "/assets/img/profile-default.svg"
	}
	return p.ImageURL
}

// IconURL selects the stored favicon or the bundled neutral icon.
func (p Profile) IconURL() string {
	if p.FaviconURL == "" {
		return "/assets/img/favicon.svg"
	}
	return p.FaviconURL
}

// ProfileState shares one application's stored profile with rendering.
// Initialization loads it from storage. Successful edits replace it atomically.
// It must not be copied or shared between applications.
type ProfileState struct {
	// saveMu preserves write order without blocking readers during storage I/O.
	saveMu sync.Mutex
	mu     sync.RWMutex
	value  Profile
}

// Current returns a copy so a request cannot modify the shared profile.
func (p *ProfileState) Current() Profile {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.value
}

// profileRepository stores the singleton site profile independently of pages.
type profileRepository interface {
	// GetProfile returns errNotFound until a profile has been saved.
	GetProfile(context.Context) (Profile, error)
	SaveProfile(context.Context, Profile) error
}

// profileEditorData retains submitted content and any save or validation error.
type profileEditorData struct {
	Profile
	Error string
}

// profileImageField supplies one image picker without repeating its template.
type profileImageField struct {
	Name, Label, Value, DefaultURL string
}

// PreviewURL shows the selected image, a neutral default, or no background.
func (f profileImageField) PreviewURL() string {
	if f.Value != "" {
		return f.Value
	}
	return f.DefaultURL
}

// ImageFields keeps image controls in the same order on full pages and validation errors.
func (p profileEditorData) ImageFields() []profileImageField {
	return []profileImageField{
		{"imageURL", "Portrait", p.ImageURL, (Profile{}).PortraitURL()},
		{"faviconURL", "Favicon", p.FaviconURL, (Profile{}).IconURL()},
		{"backgroundURL", "Background", p.BackgroundURL, ""},
	}
}

// profileEditor displays the current profile in an administrator-only form.
func (m *module) profileEditor(w http.ResponseWriter, r *http.Request) {
	m.renderProfileEditor(w, r, m.profile.Current(), "", http.StatusOK)
}

// saveProfile validates public content without changing operational configuration.
func (m *module) saveProfile(w http.ResponseWriter, r *http.Request) {
	parseError := form.Parse(r)
	p := Profile{
		Name:          strings.TrimSpace(r.PostForm.Get("name")),
		Bio:           r.PostForm.Get("bio"),
		ImageURL:      strings.TrimSpace(r.PostForm.Get("imageURL")),
		FaviconURL:    strings.TrimSpace(r.PostForm.Get("faviconURL")),
		BackgroundURL: strings.TrimSpace(r.PostForm.Get("backgroundURL")),
	}
	if parseError != nil {
		m.renderProfileEditor(w, r, p, parseError.Message, parseError.StatusCode)
		return
	}
	if err := p.validate(); err != nil {
		m.renderProfileEditor(w, r, p, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if err := m.writeProfile(r.Context(), p); err != nil {
		logger.LogRequestError(r, err)
		m.renderProfileEditor(w, r, p, "Unable to save the site profile. Please try again.", http.StatusInternalServerError)
		return
	}
	// Reload the whole document so the header, favicon, and background update together.
	util.RedirectToURL(w, r, "/profile")
}

// writeProfile serializes persistence and publication so concurrent saves cannot
// leave rendering behind the database. A failed write preserves the previous state.
func (m *module) writeProfile(ctx context.Context, p Profile) error {
	m.profile.saveMu.Lock()
	defer m.profile.saveMu.Unlock()
	if err := m.profileStore.SaveProfile(ctx, p); err != nil {
		return err
	}

	m.profile.mu.Lock()
	m.profile.value = p
	m.profile.mu.Unlock()
	m.feed.invalidate()
	return nil
}

// renderProfileEditor preserves ordinary form values and mounted HTMX inputs on errors.
func (m *module) renderProfileEditor(w http.ResponseWriter, r *http.Request, p Profile, message string, status int) {
	page := m.views["profileEditor"].Content(r, profileEditorData{Profile: p, Error: message})
	if page.IsHtmx() && status >= 400 {
		page = m.views["message"].Content(r, message)
	}
	page.Title, page.ResponseStatusCode = "Site Profile", status
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display the site profile.", err)
	}
}
