package blog

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
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
	// Version is the expected edit token. Zero creates the first stored profile.
	Version int64
}

var errProfileUnchanged = errors.New("profile content is unchanged")

// sameContent compares public content without treating an edit token as content.
func (p Profile) sameContent(other Profile) bool {
	p.Version = other.Version
	return p == other
}

// validate rejects incomplete text and unsafe image URLs before saving.
func (p Profile) validate() error {
	if strings.TrimSpace(p.Name) == "" || !utf8.ValidString(p.Name) || utf8.RuneCountInString(p.Name) > 240 {
		return writingFieldError{errors.New("Enter a name of 1–240 characters."), "name"}
	}
	if !utf8.ValidString(p.Bio) || utf8.RuneCountInString(p.Bio) > 500 {
		return writingFieldError{errors.New("Keep the short bio under 500 characters."), "bio"}
	}
	for _, image := range []struct{ name, value, field string }{
		{"Portrait", p.ImageURL, "imageURL"},
		{"Favicon", p.FaviconURL, "faviconURL"},
		{"Background", p.BackgroundURL, "backgroundURL"},
	} {
		if !utf8.ValidString(image.value) || len(image.value) > 2048 {
			return writingFieldError{errors.New(image.name + " URL must contain at most 2,048 UTF-8 bytes."), image.field}
		}
		if image.value == "" || util.SafeRedirectURL(image.value, "") == image.value {
			continue
		}
		u, err := url.Parse(image.value)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			return writingFieldError{errors.New(image.name + " URL must be a local absolute path or HTTPS URL."), image.field}
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
	// SaveProfile returns errProfileUnchanged without writing when all content matches.
	SaveProfile(context.Context, Profile) error
	ListProfileRevisions(context.Context) ([]ProfileRevision, error)
	GetProfileRevision(context.Context, int64) (ProfileRevision, error)
	// DeleteProfileRevision protects the latest snapshot and leaves current content unchanged.
	DeleteProfileRevision(context.Context, int64) error
}

// profileEditorData retains submitted content and any save or validation error.
type profileEditorData struct {
	Profile
	Error          string
	ErrorFields    string
	LoadedRevision *ProfileRevision
	HasHistory     bool
	InitialProfile bool
	ProfileCreated bool
	MissingImages  map[string]bool
}

// profileImageField supplies one image picker without repeating its template.
type profileImageField struct {
	Name, Label, Value, DefaultURL string
	Missing                        bool
}

// PreviewURL shows the selected image, a neutral default, or no background.
func (f profileImageField) PreviewURL() string {
	if f.Missing {
		return ""
	}
	if f.Value != "" {
		return f.Value
	}
	return f.DefaultURL
}

// ImageFields keeps image controls in the same order on full pages and validation errors.
func (p profileEditorData) ImageFields() []profileImageField {
	fields := []profileImageField{
		{Name: "imageURL", Label: "Portrait", Value: p.ImageURL, DefaultURL: (Profile{}).PortraitURL()},
		{Name: "faviconURL", Label: "Favicon", Value: p.FaviconURL, DefaultURL: (Profile{}).IconURL()},
		{Name: "backgroundURL", Label: "Background", Value: p.BackgroundURL},
	}
	for i := range fields {
		fields[i].Missing = p.MissingImages[imageReference(fields[i].Value, "/")]
	}
	return fields
}

// profileEditor displays the current profile in an administrator-only form.
func (m *module) profileEditor(w http.ResponseWriter, r *http.Request) {
	p := m.profile.Current()
	if raw := r.URL.Query().Get("revision"); raw != "" {
		number, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || number < 1 {
			m.storageError(w, r, errNotFound)
			return
		}
		selected, err := m.profileStore.GetProfileRevision(r.Context(), number)
		if err != nil {
			m.storageError(w, r, err)
			return
		}
		selected.Version = p.Version
		p = selected.Profile
	}
	m.renderProfileEditor(w, r, p, "", http.StatusOK)
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
	version, err := strconv.ParseInt(r.PostForm.Get("version"), 10, 64)
	if err != nil || version < 0 {
		m.renderProfileEditor(w, r, p, "The profile version is missing or invalid. Reload the editor before saving.", http.StatusBadRequest)
		return
	}
	p.Version = version
	m.saveProfileContent(w, r, p, false)
}

// saveProfileContent applies the same validation and publication rules to edits and restores.
// Failed restores open the editor so missing images or stale content can be addressed.
func (m *module) saveProfileContent(w http.ResponseWriter, r *http.Request, p Profile, restore bool) {
	if err := p.validate(); err != nil {
		m.renderProfileEditor(w, r, p, err.Error(), http.StatusUnprocessableEntity, validationFields(err)...)
		return
	}
	destination := "/profile"
	if restore {
		destination = "/profile/history"
	}
	if err := m.writeProfile(r.Context(), p); err != nil {
		if errors.Is(err, errEditConflict) {
			m.renderProfileEditor(w, r, p, editConflictMessage, http.StatusConflict)
			return
		}
		if errors.Is(err, errMissingMedia) {
			m.renderProfileEditor(w, r, p, err.Error(), http.StatusUnprocessableEntity, validationFields(err)...)
			return
		}
		logger.LogRequestError(r, err)
		m.renderProfileEditor(w, r, p, "Unable to save the site profile. Please try again.", http.StatusInternalServerError)
		return
	}
	if p.Version == 0 && !restore {
		destination = "/profile?created=1"
	}
	// Reload the whole document so the header, favicon, and background update together.
	util.RedirectToURL(w, r, destination)
}

// writeProfile serializes persistence and publication for both edits and restores.
// Failed and unchanged writes leave the shared state and caches intact.
func (m *module) writeProfile(ctx context.Context, p Profile) error {
	m.mediaMu.RLock()
	defer m.mediaMu.RUnlock()

	m.profile.saveMu.Lock()
	defer m.profile.saveMu.Unlock()
	for _, image := range []struct{ field, value string }{{"imageURL", p.ImageURL}, {"faviconURL", p.FaviconURL}, {"backgroundURL", p.BackgroundURL}} {
		if err := m.checkMediaReferences(ctx, []string{image.value}); err != nil {
			return writingFieldError{err, image.field}
		}
	}
	if err := m.profileStore.SaveProfile(ctx, p); errors.Is(err, errProfileUnchanged) {
		return nil
	} else if err != nil {
		return err
	}
	p.Version++

	m.profile.mu.Lock()
	m.profile.value = p
	m.profile.mu.Unlock()
	m.feed.invalidate()
	return nil
}

// renderProfileEditor preserves ordinary form values and mounted HTMX inputs on errors.
func (m *module) renderProfileEditor(w http.ResponseWriter, r *http.Request, p Profile, message string, status int, fields ...string) {
	data := profileEditorData{Profile: p, Error: message, ErrorFields: strings.Join(fields, " ")}
	current := m.profile.Current()
	data.InitialProfile = current.Version == 0
	// The redirect flag only selects a confirmation. Stored state determines whether it applies.
	data.ProfileCreated = r.Method == http.MethodGet && status == http.StatusOK && current.Version == 1 && r.URL.Query().Get("created") == "1" && r.URL.Query().Get("revision") == ""
	// Failed HTMX submissions leave the mounted editor and its concurrency token intact.
	view := m.views["profileEditor"]
	page := view.Content(r, data)
	if page.IsHtmx() && status >= 400 {
		page = m.views["formError"].Content(r, data)
	} else {
		versions, err := m.profileStore.ListProfileRevisions(r.Context())
		if err != nil && status < 400 {
			m.storageError(w, r, err)
			return
		}
		data.HasHistory = len(versions) > 1
		if raw := r.URL.Query().Get("revision"); raw != "" {
			for _, version := range versions {
				if strconv.FormatInt(version.ID, 10) == raw {
					data.LoadedRevision = &version
					break
				}
			}
		}
		missing, err := m.missingMedia(r.Context(), []string{p.ImageURL, p.FaviconURL, p.BackgroundURL})
		if err != nil && status < 400 {
			m.storageError(w, r, err)
			return
		}
		data.MissingImages = missing
		page = view.Content(r, data)
	}
	page.Title, page.ResponseStatusCode = "Site Profile", status
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display the site profile.", err)
	}
}
