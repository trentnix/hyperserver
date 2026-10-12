# HyperMedia Components Documentation

HyperServer implements hyperMedia components to simplify serving hypertext-based content to requestors. The implemented components reside in the **components** folder and include:

- **Content**: A view model for rendering and delivering HTML payloads.
- **ContentManagerService**: Component used to manage reusable templates for rendering pages and components.
- **SessionMessage**: A message that is serialized to a session.
- **Notification**: A message that is 
- **Form**: Validates, renders, and manages errors for HTML forms.
- **htmx.Request**: Models HTMX-related request attributes.
- **htmx.Response**: Models HTMX-related response attributes and configurations.

This document provides details on each component, its purpose, and usage.

## Content Component

The `Content` component, found in the **content** package, is a view model responsible for rendering HTML based on provided templates and data.

### Template Categories

Templates are categorized into:

- **Layouts**: Serve as scaffolding (headers, footers, and general page structure).
- **Contents**: Page-specific main content.
- **Components**: Reusable snippets used in layouts or content.

The examples use `templateFiles`, an `fs.FS` such as an `embed.FS` or an `os.DirFS` rooted at an absolute directory. Paths are relative to that filesystem. Each call can supply a different filesystem.

Example usage:

```go
homepage := content.NewContent(httpRequest /* *http.Request */)
homepage.Title = "Home Page"
homepage.AddLayout(templateFiles, "templates/layout.html")
homepage.AddContent(templateFiles, "templates/home-content.html")
homepage.AddComponent(templateFiles, "templates/footer.html")
err := homepage.Render(w, r)
```

The `Content` component contains plural methods for convenience:

```go
homepage.AddContents(templateFiles, "templates/home-content.html", "templates/admin-content.html")
homepage.AddComponents(templateFiles, "templates/footer.html", "templates/admin-components.html")
```

*Note*: When serving an HTMX request, layout templates are ignored during rendering.

### Content.Data

`Content.Data` stores any data required for rendering within templates.

### Content Manager Service

The `ContentManagerService` is a service managing global template paths and application URLs.

- Layouts, Contents, Components can be globally managed.
- Provides default URL paths (HomeURL, AuthURL, ErrorURL) configurable as needed.

The ApplicationServer structure has a `ContentManagerService` named `ContentManager` or you can create your own *ContentManager* via *content.NewContentManager*.

A `Content` instance must have its *ContentManagerService* set before rendering to use any configured templates:

```go
import (
  "github.com/trentnix/hyperserver/pkg/components/content"
  content_services "github.com/trentnix/hyperserver/pkg/services/content"
)

/* ... */

homepage := content.NewContent(httpRequest /* *http.Request */)
homepage.ContentManager = content_services.NewContentManager()
```

Alternatively, *content.NewManagedContent* can be used for convenience:

```go
cm := content_services.NewContentManager()

/* set up the ContentManagerService */

homepage := content.NewManagedContent(r /* *http.Request */, cm)
```

*ContentManagerService* also stores URLs for commonly used paths such as the home page (*HomeURL*), login page (*AuthURL*), and error page (*ErrorURL*). These values can be changed from the defaults by your module and then the application can use your custom URLs for redirection. Methods for replacing the template paths in a *ContentManagerService* or adding to the existing paths are available.

*ContentManagerService* can also store the application name and a title value to be used, and it also contains an *ErrorHandler** function variable that allows you to override default ErrorHandler behavior.

See below for an example configuring a ContentManagerService` instance:

```go
import content_services "github.com/trentnix/hyperserver/pkg/services/content"

/* ... */

cm := content_services.NewContentManager()
cm.AddPageLayout(templateFiles, "templates/some-layout.html")
cm.AddPageComponent(templateFiles, "templates/componenents/some-component.html")

cm.HomeURL = homeURL
cm.AuthURL = authURL

cm.AppName = "My Company"
cm.AppTitle = "My Application"

cm.ErrorHandler = m.RedirectToError
}

func (m *SiteModule) RedirectToError(w http.ResponseWriter, r *http.Request, message string, e error) {
  // handle redirection
}
```

Templates can be selectively added to be rendered with standard responses, HTMX response, or both:

```go
contentManager.AddPageComponent(templateFiles, "templates/componenents/some-universal-component.html")
contentManager.AddHtmxComponent(templateFiles, "templates/componenents/some-universal-component.html")
```

## Messages

The `messages` package defines values for `Content.Messages`, `Content.Notifications`, and `Content.SystemMessages`. These values require no storage or session service:

```go
message := messages.NewMessage("Your settings were saved.", messages.MessageTypeSuccess)
notification := messages.NewNotification("Your settings were saved.", messages.NotificationTypeSuccess)
systemMessage := messages.NewSystemMessage("Request completed.", messages.SystemMessageTypeInfo)
```

### Session-backed messages

The [flash service](../services/flash/messages.go) stores messages between requests. Its helpers require session middleware on the request. Check errors from both storage and retrieval.

For messages grouped by purpose, supply a category:

```go
if err := flash.AddSuccessMessage(w, r, "Your settings were saved.", "settings"); err != nil {
    return err
}

storedMessages, err := flash.GetMessages(w, r, "settings")
if err != nil {
    return err
}
page.Messages = append(page.Messages, storedMessages...)
```

Retrieval removes the category from storage before returning its messages. If removal fails, retrieval returns an error and no messages. Other categories remain available.

For notifications, use `flash.AddSuccessNotification` or `flash.AddErrorNotification`. A content manager can retrieve notifications before template execution:

```go
contentManager.LoadNotifications = flash.GetNotifications
```

Set the loader before serving requests. It must be concurrency-safe. A nil loader skips retrieval but still allows manually supplied `Content.Notifications`. A custom loader can supply notifications from another source. Loader errors stop rendering.

For browser console messages, use `flash.AddSystemDebugMessage`, `flash.AddSystemLogMessage`, `flash.AddSystemInfoMessage`, `flash.AddSystemWarningMessage`, or `flash.AddSystemErrorMessage`. Retrieve them with `flash.GetSystemMessages(w, r)` and assign them to `Content.SystemMessages`.

## form Package

Embed `form.Form` to store field errors and form-level messages. Implement `Bind` and `Validate` to satisfy `form.FormComponent`. Validation uses ordinary Go checks, not struct tags.

`FormComponent` requires only `Bind`, `Validate`, `SetFieldErrors`, and `SetValidated`. `form.Validate` accepts `ValidationForm`, without HTTP binding. `ParseAndValidate` and `Validate` reject nil forms but accept any implementation of their interfaces, including value wrappers around shared form state. `HandleFormError` accepts an `ErrorForm` with only `AddErrorMessage`.

Applications can supply their own form storage and template accessors without implementing unused message methods. Check recorded errors before changing application data. Forms embedding `Form` can call `HasErrors`.

For example, a contact form can define its rules alongside its fields:

```go
type ContactForm struct {
    Name    string
    Email   string
    Message string

    form.Form
}

func (f *ContactForm) Bind(r *http.Request) error {
    f.Name = r.FormValue("name")
    f.Email = r.FormValue("email")
    f.Message = r.FormValue("message")
    return nil
}

func (f *ContactForm) Validate() form.FieldErrors {
    errors := make(form.FieldErrors)
    if f.Name == "" {
        errors["Name"] = []string{form.RequiredMessage}
    }
    if f.Email == "" {
        errors["Email"] = []string{form.RequiredMessage}
    } else if !form.ValidEmail(f.Email) {
        errors["Email"] = []string{form.EmailMessage}
    }
    if f.Message == "" {
        errors["Message"] = []string{form.RequiredMessage}
    }
    return errors
}
```

`Validate` returns plain-text messages keyed by Go field name. It must not change form state or application data. A nil or empty map means no field failed validation. See [account validation](../../auth/modules/email/form_validation.go) for forms that share password rules with account operations.

### Rendering a Form

A `Content` instance renders the form. These examples use a configured `contentManager` and the `templateFiles` filesystem described above:

```go
page := content.NewManagedContent(r, contentManager)
page.PartialName = "contact-form"
page.AddContent(templateFiles, "templates/forms/contact-form.html")
page.Data = &ContactForm{}
if err := page.Render(w, r); err != nil {
    contentManager.HandleError(w, r, "Unable to render the contact form", err, http.StatusInternalServerError)
}
```

### Validate a Form

In the POST handler, parse and validate before saving data or sending mail. `ParseAndValidate` enforces form limits, calls `Bind`, and records the errors returned by `Validate`. A nil returned error does not mean the form is valid. Callers must also check `HasErrors()`:

```go
page := content.NewManagedContent(r, contentManager)
page.PartialName = "contact-form"
page.AddContent(templateFiles, "templates/forms/contact-form.html")
contactForm := &ContactForm{}

message, err := form.ParseAndValidate(r, contactForm)
if err != nil {
    form.HandleFormError(w, r, page, contactForm, message, err)
    return
}

if contactForm.HasErrors() {
    form.HandleFormError(w, r, page, contactForm, "", nil)
    return
}

// Save the validated submission and handle the operation's result here.
```

`form.HandleFormError` renders field and form-level errors. Parsing failures return the parser's HTTP status without rendering submitted values. Other supplied errors are logged before rendering. Render all messages through `html/template` to escape their contents.

### Form HTML Template

The example template defines the `contact-form` partial and displays errors beside each field:

```html
{{template "contact-form" .}}

{{define "contact-form"}}
{{ with .Data }}
    <form id="contact-form"
          method="post"
          action="/contact"
          hx-post="/contact"
          hx-target="this"
          hx-swap="outerHTML"
          hx-trigger="submit">
        
        <label for="name">Name</label>
        <input type="text" id="name" name="name" value="{{ .Name }}"
               required {{ if .HasFieldErrors "Name" }} aria-invalid="true" {{ end }}>
        {{template "field-errors" (.GetFieldErrors "Name")}}

        <label for="email">Email</label>
        <input type="email" id="email"
               name="email"
               value="{{ .Email }}"
               required {{ if .HasFieldErrors "Email" }} aria-invalid="true" {{ end }}>
        {{template "field-errors" (.GetFieldErrors "Email")}}

        <label for="message">Message</label>
        <textarea id="message" name="message"
                  required {{ if .HasFieldErrors "Message" }} aria-invalid="true" {{ end }}>{{ .Message }}</textarea>
        {{template "field-errors" (.GetFieldErrors "Message")}}

        <button type="submit">Send message</button>
        
        {{ if .HasErrorMessages }}
            <div class="form-messages"><ul>{{ range .GetErrorMessages }}<li>{{ .Message }}</li>{{ end }}</ul></div>
        {{ else if .HasInfoMessages }}
            <div class="form-messages"><ul>{{ range .GetInfoMessages }}<li>{{ .Message }}</li>{{ end }}</ul></div>
        {{ end }}
    </form>
{{ else }}
    <div>Unable to display the contact form.</div>
{{ end }}
{{end}}

{{define "field-errors"}}
    {{- range .}}
        <span class="field-error">{{.}}</span>
    {{- end}}
{{end}}
```

## htmx Package

The **htmx** package simplifies interactions with HTMX requests and responses.

### HTMX Request

The `htmx.Request` component extracts HTMX request details easily:

```go
htmxRequest := htmx.GetRequest(r)
fmt.Printf("trigger: %s", htmxRequest.Trigger)
```

### HTMX Reponse

To create and configure a `htmx.Response`:

```go
htmxResp := htmx.NewResponse()
htmxResp.TriggerAfterSwap = "contentUpdated"
htmxResp.Apply(w)
```

---

This documentation aims to provide clarity about the framework's core components, their purposes, and usage patterns, aiding developers in efficiently building HyperMedia applications.
