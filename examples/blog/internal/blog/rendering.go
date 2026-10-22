package blog

import (
	"bytes"
	"errors"
	"html/template"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/content"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/yuin/goldmark"
)

const (
	pagesFile    = "templates/pages.html"
	responseView = "blog.message"
	loginURL     = "/auth/login/email"
)

// ContentSecurityPolicy permits bundled scripts and the original site's Google fonts.
// Editor-generated inline styles use style attributes, not inline scripts.
const ContentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self' https://fonts.googleapis.com; style-src-attr 'unsafe-inline'; font-src 'self' https://fonts.gstatic.com; img-src 'self' https:; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

// ConfigureRendering selects the blog layout for feature and authentication pages.
// Call it before module initialization prepares views. The registration flag
// permits a registration link only while the administrator account is absent.
// Account admission and route availability remain the authentication provider's job.
func ConfigureRendering(cm *contentservice.ContentManagerService, access *Access, accounts user.AccountRepository, registration bool) error {
	if err := cm.ApplyDefaults("blog", func(cm *contentservice.ContentManagerService) {
		cm.RegisterLayouts(contentservice.PageType, blogModuleFS, "templates/layout.html")
		cm.RegisterLayouts(contentservice.HtmxType, blogModuleFS, "templates/fragment.html")
		cm.RegisterComponents(contentservice.PageType, blogModuleFS, "templates/profile.html", "templates/notifications.html")
		cm.RegisterComponents(contentservice.HtmxType, blogModuleFS, "templates/notifications.html")
		cm.AppTitle, cm.HomeURL, cm.AuthURL = "", "/", loginURL
		cm.BuildLayoutData = func(r *http.Request) any {
			account := user.GetUserFromContext(r.Context())
			data := navigation{SignedIn: account != nil, Admin: access.IsAdmin(account)}
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
		Message: responseView, Error: responseView, NotFound: responseView,
		NotFoundMessage: "That page could not be found.",
	}, content.TemplatePath{FS: blogModuleFS, Path: pagesFile})
}

// navigation holds account links for the current request, not cached account state.
type navigation struct{ SignedIn, Admin, Registration bool }

// render uses a view prepared during initialization, selecting a full page or
// HTMX fragment from the request. Post pages also supply the document title.
func (m *module) render(w http.ResponseWriter, r *http.Request, name string, data any) {
	page := m.views[name].Content(r, data)
	if post, ok := data.(postPage); ok {
		page.Title = post.Title
	}
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this page.", err)
	}
}

// markdown is the only trust boundary for stored post HTML. Goldmark omits raw
// HTML and unsafe URL schemes. Preview and published posts use the same renderer.
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
