package blog

import (
	"errors"
	"net/http"
	"strings"

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
	loginURL      = "/login"
	authReturnURL = "/auth/continue"
)

// ContentSecurityPolicy permits bundled assets. Deployment configuration supplies
// additional form-action origins for external authentication redirects.
// Editor-generated inline styles use style attributes, not inline scripts.
const ContentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; style-src-attr 'unsafe-inline'; font-src 'self'; img-src 'self' https:; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

// configureRendering selects shared layouts and installs one navigation callback.
// Site owns the profile and administrator rule used by rendering and authentication.
func (s *Site) configureRendering(cm *contentservice.ContentManagerService, accounts user.AccountRepository, registration bool) error {
	profile, access := &s.profile, &s.access
	if err := cm.ApplyDefaults("blog", func(cm *contentservice.ContentManagerService) {
		cm.RegisterLayouts(contentservice.PageType, blogModuleFS, "templates/layout.html")
		cm.RegisterLayouts(contentservice.HtmxType, blogModuleFS, "templates/fragment.html")
		cm.RegisterComponents(contentservice.PageType, blogModuleFS, "templates/page-components.html", "templates/profile.html", "templates/notifications.html", "templates/auth.html", "templates/archive.html", "templates/metadata.html", "templates/album.html")
		cm.RegisterComponents(contentservice.HtmxType, blogModuleFS, "templates/profile.html", "templates/notifications.html", "templates/auth.html", "templates/archive.html", "templates/metadata.html")
		// Authentication returns through setup before selecting login, profile, or home.
		cm.AppTitle, cm.HomeURL, cm.AuthURL = "", authReturnURL, loginURL
		cm.BuildLayoutData = func(r *http.Request) any {
			account := user.GetUserFromContext(r.Context())
			permission, roleErr := access.Role(r.Context(), account)
			if roleErr != nil {
				logger.LogRequestError(r, roleErr)
			}
			data := navigation{Profile: profile.Current(), SignedIn: account != nil, NeedsVerification: account != nil && account.NeedsVerification(), Admin: permission == roleAdmin, Writer: permission.valid()}
			data.Invitation = r.Context().Value(invitationPageKey{}) != nil
			data.UserManagement = data.Admin && access.multiUser
			if data.Admin && !access.multiUser {
				users, err := access.administration.ListUsers(r.Context())
				if err != nil {
					logger.LogRequestError(r, err)
				}
				data.UserManagement = len(users) > 1
			}
			loginPage := r.URL.Path == loginURL
			registerPage := r.URL.Path == "/register"
			if r.Context().Value(setupPageKey{}) != nil || access.setup != nil && !access.setup.complete.Load() && (loginPage || registerPage || strings.HasPrefix(r.URL.Path, "/auth/")) {
				data.Setup = true
				data.Profile = Profile{Name: "My Blog"}
				switch {
				case registerPage:
					data.SetupTitle = "Set up your blog"
					data.SetupHelp = "Register using the administrator email from your configuration."
				case loginPage:
					data.SetupTitle = "Sign in to your blog"
					data.SetupHelp = "Sign in to edit your site profile."
					if account, err := accounts.GetByEmail(r.Context(), access.emailKey); err == nil && account.NeedsVerification() {
						data.SetupTitle = "Check your email"
						data.SetupHelp = "Open the verification link in your email to make your site public."
						data.SetupVerificationPending = true
						data.SetupLoginOpen = r.Method == http.MethodPost
					}
				}
				return data
			}
			data.ReaderSearch = r.Context().Value(adminPageKey{}) == nil && !loginPage && !registerPage && !strings.HasPrefix(r.URL.Path, "/auth/")
			data.Description = data.Profile.Bio
			if data.Writer {
				data.EditorOwner = account.ID
			}
			cookie, err := r.Cookie(returningUserCookie)
			data.ShowLogin = !data.SignedIn && err == nil && cookie.Value == "1"
			if loginPage || registerPage {
				data.ShowLogin = false
			}
			if registration && !data.SignedIn {
				_, err := accounts.GetByEmail(r.Context(), access.emailKey)
				data.Registration = errors.Is(err, user.ErrUserNotFound)
				if err != nil && !data.Registration {
					logger.LogRequestError(r, err)
				}
			}
			hx := htmx.GetRequest(r)
			if s.populateNavigation != nil && (!hx.Enabled || hx.HistoryRestore || hx.FullPage) {
				data = s.populateNavigation(r, data)
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
		content.TemplatePath{FS: blogModuleFS, Path: "templates/users.html"},
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
	Setup                                    bool
	SetupVerificationPending, SetupLoginOpen bool
	SetupTitle, SetupHelp                    string
	Profile                                  Profile
	Sharing                                  *sharingPreview
	Pages                                    []pageLink
	Recent                                   []ArchiveEntry
	RecentUnavailable                        bool
	ReaderSearch                             bool
	EditorOwner, Description                 string
	SignedIn, Admin, Registration, ShowLogin bool
	Writer                                   bool
	NeedsVerification                        bool
	Invitation                               bool
	UserManagement                           bool
	AccountControls                          bool
}

// populateNavigation loads shell links for full pages and explicit navigation updates.
func (m *module) populateNavigation(r *http.Request, data navigation) navigation {
	var err error
	if data.SignedIn && m.auth != nil {
		methods, err := m.auth.AccountMethods(r.Context(), user.GetUserFromContext(r.Context()))
		if err != nil {
			// Keep Account reachable so its page can report the failed service.
			data.AccountControls = true
			logger.LogRequestError(r, err)
		}
		for _, method := range methods {
			data.AccountControls = data.AccountControls || len(method.Actions) > 0
		}
	}
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
