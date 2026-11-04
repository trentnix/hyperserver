package web_test

import (
	"context"
	"database/sql"
	"embed"
	"io/fs"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/htmx"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/ratelimit"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// These fixtures exercise HTTP contracts without loading a production application.
// Storage is deliberately small. Provider durability is tested in provider packages.
//
//go:embed testdata/templates testdata/js
var fixtureTemplates embed.FS

type contactSubmission struct {
	Id, Name, Email, Message string
	CreatedAt                time.Time
}

type contactWriter func(context.Context, *contactSubmission) error

func (write contactWriter) Create(ctx context.Context, submission *contactSubmission) error {
	return write(ctx, submission)
}

func sqlContacts(ctx context.Context, db *sql.DB) (contactWriter, error) {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS hyperserver_contact_submission (
		id TEXT PRIMARY KEY, name TEXT, email TEXT, message TEXT, created_at DATETIME)`)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, submission *contactSubmission) error {
		created := *submission
		created.Id, created.CreatedAt = uuid.NewString(), time.Now()
		_, err := db.ExecContext(ctx, `INSERT INTO hyperserver_contact_submission (id,name,email,message,created_at) VALUES (?,?,?,?,?)`, created.Id, created.Name, created.Email, created.Message, created.CreatedAt)
		if err == nil {
			*submission = created
		}
		return err
	}, nil
}

type pageRenderer struct {
	contentManager *contentservice.ContentManagerService
	views          map[string]*content.View
	errorTemplates *content.Templates
}

type formFixture struct {
	pageRenderer
	settings struct {
		RateLimit *config.ModuleRateLimitConfig `yaml:"rateLimit"`
	}
	contacts   contactWriter
	mailClient *messaging.MailClient
	auth       *auth.AuthManager
}

func (m *formFixture) Configuration() any { return &m.settings }

func formModule(write contactWriter, mail *messaging.MailClient) handlers.Descriptor {
	return handlers.Descriptor{Name: "forms", Requires: []handlers.Requirement{{Capability: auth.ManagerCapability, Optional: true}}, New: func() handlers.Handler {
		return &formFixture{contacts: write, mailClient: mail}
	}}
}

func (m *formFixture) BindDependencies(d handlers.Dependencies) error {
	var err error
	m.auth, err = handlers.GetDependency[*auth.AuthManager](d, auth.ManagerCapability)
	return err
}

func (m *formFixture) Init(_ context.Context, app *server.ApplicationServer) error {
	m.contentManager = app.ContentManager
	if err := m.prepareTemplates(); err != nil {
		return err
	}
	for _, page := range []struct{ file, partial string }{
		{homePageTemplate, homePagePartialName}, {contactPageTemplate, contactPagePartialName}, {contactFormTemplate, contactFormPartialName},
	} {
		files := []content.TemplatePath{{FS: fixtureTemplates, Path: page.file}}
		if page.file == contactPageTemplate {
			files = append(files, content.TemplatePath{FS: fixtureTemplates, Path: contactFormTemplate})
		}
		view, err := content.PrepareView(m.contentManager, page.partial, files...)
		if err != nil {
			return err
		}
		m.views[page.partial] = view
	}
	return nil
}

func (m *formFixture) Routes(routes *routing.Routes) error {
	if m.auth != nil && m.auth.Enabled {
		pages, err := m.auth.PageHandlers(routes)
		if err != nil {
			return err
		}
		routes.Handle("GET /login", pages.Login)
		pages.Submissions.Handle("POST /login", pages.LoginSubmit)
		routes.Handle("GET /register", pages.Registration)
		pages.Submissions.Handle("POST /register", pages.RegistrationSubmit)
	}
	capacity, err := m.settings.RateLimit.ClientCapacity()
	if err != nil {
		return err
	}
	submissions, err := routes.WithRateLimit(ratelimit.Policy{Requests: 10, Window: time.Minute, MaxClients: capacity})
	if err != nil {
		return err
	}
	files, err := fs.Sub(fixtureTemplates, "testdata/js")
	if err != nil {
		return err
	}
	routes.Handle("GET /js/", http.StripPrefix("/js/", http.FileServerFS(files)))
	routes.HandleFunc("GET /js/htmx-errors.js", htmx.ServeErrorScript)
	routes.HandleFunc("GET /css/site.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Write([]byte(`body{font-family:sans-serif} .field-error{color:#b00}
		input[type=email],input[inputmode=email],input[type=password]{display:block;width:100%;padding:.5rem;margin-bottom:.75rem;box-sizing:border-box}`))
	})
	routes.HandleFunc("/", m.Home)
	routes.HandleFunc("/error", m.Error)
	routes.HandleFunc("/404", m.NotFound)
	routes.HandleFunc("GET /contact", m.GetContact)
	submissions.HandleFunc("POST /contact", m.Contact)
	policy := func(r *http.Request) (bool, error) {
		u := user.GetUserFromContext(r.Context())
		return u != nil && !u.NeedsVerification(), nil
	}
	submissions.Handle("POST /test-email", middleware.RequireAuthorization(policy)(http.HandlerFunc(m.TestEmail)))
	routes.HandleFunc("POST /session-example", m.SessionExample)
	routes.WithoutRateLimit().HandleFunc("POST /logout", m.Logout)
	return routes.Err()
}

const (
	pageLayoutTemplate               = "testdata/templates/layouts/site.html"
	partialLayoutTemplate            = "testdata/templates/layouts/partial.html"
	navComponentTemplate             = "testdata/templates/components/nav.html"
	notificationsComponentTemplate   = "testdata/templates/components/notifications.html"
	consoleMessagesComponentTemplate = "testdata/templates/components/console-messages.html"
	homePageTemplate                 = "testdata/templates/pages/home.html"
	errorPageTemplate                = "testdata/templates/pages/error.html"
	messagePageTemplate              = "testdata/templates/pages/message.html"
	notFoundPageTemplate             = "testdata/templates/pages/not-found.html"
	homePagePartialName              = "site.page.home"
	errorPagePartialName             = "site.page.error"
	messagePagePartialName           = "site.page.message"
	notFoundPagePartialName          = "site.page.not_found"
	contactPagePartialName           = "site.page.contact"
	contactFormPartialName           = "site.partial.contact.form"
	homeURL                          = "/"
	authURL                          = "/login"
)
