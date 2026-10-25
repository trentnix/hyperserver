package blog

import (
	"bytes"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/trentnix/hyperserver/pkg/components/content"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
	"github.com/yuin/goldmark"
)

const (
	pagesFile    = "templates/pages.html"
	responseView = "blog.message"
	notFoundView = "blog.notFound"
	loginURL     = "/auth/login/email"
)

// ContentSecurityPolicy permits bundled scripts, styles, and fonts.
// Editor-generated inline styles use style attributes, not inline scripts.
const ContentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; style-src-attr 'unsafe-inline'; font-src 'self'; img-src 'self' https:; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

// Profile supplies the public identity used by shared layouts and RSS.
// ImageURL can be a local absolute path or an HTTPS URL. Empty uses a neutral portrait.
type Profile struct {
	Name     string `yaml:"name"`
	Bio      string `yaml:"bio"`
	ImageURL string `yaml:"imageURL"`
}

// validate rejects incomplete or unsafe profile configuration at startup.
func (p Profile) validate() error {
	if strings.TrimSpace(p.Name) == "" || !utf8.ValidString(p.Name) || utf8.RuneCountInString(p.Name) > 240 {
		return errors.New("blog.profile.name must contain 1–240 characters")
	}
	if !utf8.ValidString(p.Bio) || utf8.RuneCountInString(p.Bio) > 500 {
		return errors.New("blog.profile.bio must contain at most 500 characters")
	}
	if p.ImageURL == "" || util.SafeRedirectURL(p.ImageURL, "") == p.ImageURL {
		return nil
	}
	u, err := url.Parse(p.ImageURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return errors.New("blog.profile.imageURL must be a local absolute path or HTTPS URL")
	}
	return nil
}

// PortraitURL selects the configured profile image or the bundled neutral image.
func (p Profile) PortraitURL() string {
	if p.ImageURL == "" {
		return "/assets/img/profile-default.svg"
	}
	return p.ImageURL
}

// ConfigureRendering selects the blog layout for feature and authentication pages.
// Call it before module initialization prepares views. The registration flag
// permits a registration link only while the administrator account is absent.
// Account admission and route availability remain the authentication provider's job.
// settings must be the same application-local value supplied to Module.
func ConfigureRendering(cm *contentservice.ContentManagerService, settings *Settings, access *Access, accounts user.AccountRepository, registration bool) error {
	if settings == nil {
		return errors.New("blog rendering requires settings")
	}
	if err := cm.ApplyDefaults("blog", func(cm *contentservice.ContentManagerService) {
		cm.RegisterLayouts(contentservice.PageType, blogModuleFS, "templates/layout.html")
		cm.RegisterLayouts(contentservice.HtmxType, blogModuleFS, "templates/fragment.html")
		cm.RegisterComponents(contentservice.PageType, blogModuleFS, "templates/profile.html", "templates/notifications.html")
		cm.RegisterComponents(contentservice.HtmxType, blogModuleFS, "templates/notifications.html")
		cm.AppTitle, cm.HomeURL, cm.AuthURL = "", "/", loginURL
		cm.BuildLayoutData = func(r *http.Request) any {
			account := user.GetUserFromContext(r.Context())
			data := navigation{Profile: settings.Profile, SignedIn: account != nil, Admin: access.IsAdmin(account)}
			cookie, err := r.Cookie(returningUserCookie)
			data.ShowLogin = !data.SignedIn && err == nil && cookie.Value == "1"
			if registration && !data.SignedIn {
				_, err := accounts.GetByEmail(r.Context(), access.emailKey)
				data.Registration = errors.Is(err, user.ErrUserNotFound)
				if err != nil && !data.Registration {
					logger.LogRequestError(r, err)
				}
			}
			return data
		}
	}); err != nil {
		return err
	}
	return content.ConfigureResponses(cm, content.ResponseTemplates{
		Message: responseView, Error: responseView, NotFound: notFoundView,
	}, content.TemplatePath{FS: blogModuleFS, Path: pagesFile})
}

// navigation holds account links for the current request, not cached account state.
type navigation struct {
	Profile                                  Profile
	SignedIn, Admin, Registration, ShowLogin bool
}

// markdown is the trust boundary for rendered blog content. Goldmark omits raw
// HTML and unsafe URL schemes. Previews and published pages use the same renderer.
func markdown(source string) (template.HTML, error) {
	var out bytes.Buffer
	if err := goldmark.New().Convert([]byte(source), &out); err != nil {
		return "", err
	}
	return template.HTML(out.String()), nil
}

// postPage pairs stored post data with HTML produced by the Markdown renderer.
type postPage struct {
	Post
	HTML template.HTML
}

// editorPage retains a draft and any user-facing error for the editing form.
type editorPage struct {
	Post
	Error string
}

// archiveMonth contains the heading and post links for one archive month.
type archiveMonth struct {
	Name  string
	Posts []ArchiveEntry
}

// groupArchive groups adjacent entries by publication month, preserving order.
// Posts must already be sorted by publication time.
func groupArchive(posts []ArchiveEntry) []archiveMonth {
	var months []archiveMonth
	for _, post := range posts {
		name := post.Published.Format("January 2006")
		if len(months) == 0 || months[len(months)-1].Name != name {
			months = append(months, archiveMonth{Name: name})
		}
		last := &months[len(months)-1]
		last.Posts = append(last.Posts, post)
	}
	return months
}
