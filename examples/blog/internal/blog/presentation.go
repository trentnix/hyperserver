package blog

import (
	"bytes"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/htmx"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/yuin/goldmark"
	"golang.org/x/net/html"
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
		cm.RegisterComponents(contentservice.PageType, blogModuleFS, "templates/profile.html", "templates/notifications.html", "templates/auth.html", "templates/archive.html")
		cm.RegisterComponents(contentservice.HtmxType, blogModuleFS, "templates/notifications.html", "templates/auth.html", "templates/archive.html")
		cm.AppTitle, cm.HomeURL, cm.AuthURL = "", "/", loginURL
		cm.BuildLayoutData = func(r *http.Request) any {
			account := user.GetUserFromContext(r.Context())
			data := navigation{Profile: profile.Current(), SignedIn: account != nil, Admin: access.IsAdmin(account)}
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
		content.TemplatePath{FS: blogModuleFS, Path: "templates/pages.html"},
		content.TemplatePath{FS: blogModuleFS, Path: "templates/media.html"},
		content.TemplatePath{FS: blogModuleFS, Path: "templates/history.html"},
		content.TemplatePath{FS: blogModuleFS, Path: "templates/profile-editor.html"},
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
		if !hx.Enabled || hx.HistoryRestore {
			var err error
			data.Recent, err = m.loadRecent(r.Context())
			if err != nil {
				data.RecentUnavailable = true
				logger.LogRequestError(r, err)
			}
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

// markdownAt renders content for previews and feeds, resolving relative links
// and images against the content URL rather than the page displaying the preview.
// The shared Markdown renderer still removes raw HTML and unsafe URL schemes.
func markdownAt(source string, base *url.URL) (template.HTML, error) {
	rendered, err := markdown(source)
	if err != nil {
		return "", err
	}
	tokens := html.NewTokenizer(strings.NewReader(string(rendered)))
	var out strings.Builder
	for {
		switch tokens.Next() {
		case html.ErrorToken:
			if tokens.Err() == io.EOF {
				return template.HTML(out.String()), nil
			}
			return "", tokens.Err()
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokens.Token()
			for i, attr := range token.Attr {
				if attr.Val == "" || !(token.Data == "a" && attr.Key == "href" || token.Data == "img" && attr.Key == "src") {
					continue
				}
				ref, err := url.Parse(attr.Val)
				if err == nil && !ref.IsAbs() {
					token.Attr[i].Val = base.ResolveReference(ref).String()
				}
			}
			out.WriteString(token.String())
		default:
			out.Write(tokens.Raw())
		}
	}
}

// postPage pairs stored post data with HTML produced by the Markdown renderer.
type postPage struct {
	Post
	HTML       template.HTML
	HasHistory bool
	Scheduled  bool
}

// editorPage retains a draft and any user-facing error for the editing form.
type editorPage struct {
	Post
	revisionSelection
	Error                      string
	PublishedSlug              string
	ScheduledSlug              string
	ScheduledAt                int64
	Publication                string
	ScheduleDate, ScheduleTime string
	CanSchedule                bool
}

// archivePage contains one bounded page of month-grouped links and adjacent URLs.
type archivePage struct {
	Months         []archiveMonth
	Number         int
	Previous, Next string
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
