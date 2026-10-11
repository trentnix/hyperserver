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

The `Form` component, defined in the **form** package, streamlines form validation, error handling, and rendering for custom forms.

See below for an example of a user registration form:

```go
RegisterForm struct {
  Email         string `validate:"required,email"`
  Password      string `validate:"required,password"`
  PasswordMatch string `validate:"required,password,eqfield=Password"`
  form.Form
}
```

Embedding `form.Form` ensures your form implements the `form.FormComponent` interface.

### Rendering a Form

A `Form` is rendered in a `Content` instance:

```go
register := content.NewManagedContent(r)
register.AddContent(templateFiles, "templates/forms/register-form.html")
register.Data = &RegisterForm{}
err := register.Render(w, r)
```

### Validate a Form

To validate a form and render any errors:

```go
r.ParseForm();
registerForm := &RegisterForm{
  Email:         r.FormValue("email"),
  Password:      r.FormValue("password"),
  PasswordMatch: r.FormValue("passwordMatch"),
}

if err := form.Validate(registerForm); err != nil {
  register := content.NewManagedContent(r)
  register.AddContent(templateFiles, "templates/forms/register-form.html")

  content.HandleFormError(w, r, register, registerForm, "The registration form could not be validated", err)
  return
}
```

`content.HandleFormError` is an error handler that will re-render the form with any errors. If an error instance is specified, the error will be logged.

### Form HTML Template

The form template used in the previous example code can be seen below:

```html
{{ with .Data }}
    <form id="register-form"
          class="auth-email-form"
          hx-post="/auth/register/email"
          hx-target="this"
          hx-swap="outerHTML"
          hx-trigger="submit">
        
        <label for="email">Email</label>
        <input type="email" id="email"
               name="email"
               value="{{ .Email }}"
               required {{ if .HasFieldErrors "Email" }} aria-invalid="true" {{ end }}>
        {{template "field-errors" (.GetFieldErrors "Email")}}

        <label for="password">Password</label>
        <input type="password" id="password"
               name="password"
               value="{{ .Password }}"
               required
               {{ if .HasFieldErrors "Password" }} aria-invalid="true" {{ end }}>
        {{template "field-errors" (.GetFieldErrors "Password")}}

        <label for="passwordMatch">Confirm Password</label>
        <input type="password" id="passwordMatch" 
               name="passwordMatch" 
               value="{{ .PasswordMatch }}" 
               required 
               {{ if .HasFieldErrors "PasswordMatch" }} class="input-error" {{ end }}>
        {{template "field-errors" (.GetFieldErrors "PasswordMatch")}}

        <button type="submit">Register</button>
        
        {{ if .HasErrorMessages }}
            <div class="form-messages"><ul>{{ range .GetErrorMessages }}<li>{{ .Message }}</li>{{ end }}</ul></div>
        {{ else if .HasInfoMessages }}
            <div class="form-messages"><ul>{{ range .GetInfoMessages }}<li>{{ .Message }}</li>{{ end }}</ul></div>
        {{ end }}
    </form>
{{ else }}
    <div>Unable to display registration details.</div>
{{ end }}

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
