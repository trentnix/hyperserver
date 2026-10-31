package blog

import (
	"errors"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/htmx"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

const (
	responsesFile = "templates/responses.html"
	responseView  = "blog.message"
	notFoundView  = "blog.notFound"
	loginURL      = "/auth/login/email"
)

// ContentSecurityPolicy permits bundled scripts, styles, and fonts.
// Editor-generated inline styles use style attributes, not inline scripts.
const ContentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; style-src-attr 'unsafe-inline'; font-src 'self'; img-src 'self' https:; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

// ConfigureRendering selects the blog layout for feature and authentication pages.
// Call it before module initialization prepares views. The registration flag
// permits a registration link only while the administrator account is absent.
// Account admission and route availability remain the authentication provider's job.
// profile must be the same application-local state supplied to Module.
func ConfigureRendering(cm *contentservice.ContentManagerService, profile *ProfileState, access *Access, accounts user.AccountRepository, registration bool) error {
	if profile == nil {
		return errors.New("blog rendering requires profile state")
	}
	if err := cm.ApplyDefaults("blog", func(cm *contentservice.ContentManagerService) {
		cm.RegisterLayouts(contentservice.PageType, blogModuleFS, "templates/layout.html")
		cm.RegisterLayouts(contentservice.HtmxType, blogModuleFS, "templates/fragment.html")
		cm.RegisterComponents(contentservice.PageType, blogModuleFS, "templates/profile.html", "templates/notifications.html", "templates/auth.html", "templates/archive.html", "templates/metadata.html")
		cm.RegisterComponents(contentservice.HtmxType, blogModuleFS, "templates/profile.html", "templates/notifications.html", "templates/auth.html", "templates/archive.html", "templates/metadata.html")
		cm.AppTitle, cm.HomeURL, cm.AuthURL = "", "/", loginURL
		cm.BuildLayoutData = func(r *http.Request) any {
			account := user.GetUserFromContext(r.Context())
			data := navigation{Profile: profile.Current(), SignedIn: account != nil, Admin: access.IsAdmin(account)}
			data.ReaderSearch = r.Context().Value(adminPageKey{}) == nil
			data.Description = data.Profile.Bio
			if data.Admin {
				data.EditorOwner = account.ID
			}
			cookie, err := r.Cookie(returningUserCookie)
			data.ShowLogin = !data.SignedIn && err == nil && cookie.Value == "1"
			switch r.URL.Path {
			case "/auth/login", loginURL, "/auth/register", "/auth/register/email":
				data.ShowLogin = false
			}
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
	}, content.TemplatePath{FS: blogModuleFS, Path: responsesFile})
}

// prepareTemplates parses the blog's feature templates together at startup so
// named components can be shared without rereading files for each request.
func prepareTemplates(cm *contentservice.ContentManagerService) (*content.Templates, error) {
	return content.PrepareTemplates(cm,
		content.TemplatePath{FS: blogModuleFS, Path: responsesFile},
		content.TemplatePath{FS: blogModuleFS, Path: "templates/posts.html"},
		content.TemplatePath{FS: blogModuleFS, Path: "templates/post-editor.html"},
		content.TemplatePath{FS: blogModuleFS, Path: "templates/page-editor.html"},
		content.TemplatePath{FS: blogModuleFS, Path: "templates/search.html"},
		content.TemplatePath{FS: blogModuleFS, Path: "templates/pages.html"},
		content.TemplatePath{FS: blogModuleFS, Path: "templates/media.html"},
		content.TemplatePath{FS: blogModuleFS, Path: "templates/history.html"},
		content.TemplatePath{FS: blogModuleFS, Path: "templates/profile-editor.html"},
		content.TemplatePath{FS: blogModuleFS, Path: "templates/profile-history.html"},
		content.TemplatePath{FS: blogModuleFS, Path: "templates/editor-components.html"},
	)
}

// navigation holds the profile, links, and sharing metadata for the current request.
type navigation struct {
	Profile                                  Profile
	Sharing                                  *sharingPreview
	Pages                                    []pageLink
	Recent                                   []ArchiveEntry
	RecentUnavailable                        bool
	ReaderSearch                             bool
	EditorOwner, Description                 string
	SignedIn, Admin, Registration, ShowLogin bool
}

// configurePageNavigation adds published page links and cached Recent Posts after storage opens.
// ConfigureRendering supplies the base navigation, including authentication links.
func (m *module) configurePageNavigation() {
	build := m.app.ContentManager.BuildLayoutData
	m.app.ContentManager.BuildLayoutData = func(r *http.Request) any {
		data := build(r).(navigation)
		hx := htmx.GetRequest(r)
		if hx.Enabled && !hx.HistoryRestore {
			return data
		}
		return m.populateNavigation(r, data)
	}
}

// populateNavigation loads shell links for full pages and explicit navigation updates.
func (m *module) populateNavigation(r *http.Request, data navigation) navigation {
	var err error
	data.Recent, err = m.loadRecent(r.Context())
	if err != nil {
		data.RecentUnavailable = true
		logger.LogRequestError(r, err)
	}
	pages, err := m.pageStore.ListPages(r.Context(), false)
	if err != nil {
		logger.LogRequestError(r, err)
	} else {
		data.Pages = pageTree(pages, nil)
		for i := range data.Pages {
			for j := range data.Pages[i].Children {
				data.Pages[i].Children[j].Children = nil
			}
		}
	}
	return data
}
