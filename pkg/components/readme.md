# HyperMedia Components Documentation

The HyperMedia Components framework simplifies serving hypertext-based content to clients by abstracting common tasks. The implemented components reside in the **components** folder and include:

- **Content**: A view model for rendering HTML views.
- **ContentManager**: Manages reusable templates for rendering pages and components.
- **ContentMessage**: Manages session-based messaging.
- **Form**: Handles validation and error management for forms.
- **htmx.Request**: Stores HTMX-related request attributes.
- **htmx.Response**: Stores HTMX-related response attributes and configurations.

This document provides details on each component, its purpose, and usage.

## Content Component

The `Content` component, found in the **content** package, is a view model responsible for rendering HTML based on provided templates and data.

### Template Categories

Templates are categorized into:

- **Layouts**: Serve as scaffolding (headers, footers, and general page structure).
- **Contents**: Page-specific main content.
- **Components**: Reusable snippets used in layouts or content.

Example usage:

```go
homepage := content.NewContent(httpRequest /* *http.Request */)
homepage.Title = "Home Page"
homepage.AddLayout("templates/layout.html")
homepage.AddContent("templates/home-content.html")
homepage.AddComponent("templates/footer.html")
err := homepage.Render(w, r)
```

The `Content` component contains plural methods for convenience:

```go
homepage.AddContents("templates/home-content.html", "templates/admin-content.html")
homepage.AddComponents("templates/footer.html", "templates/admin-components.html")
```

*Note*: When serving an HTMX request, layout templates are ignored during rendering.

### Content.Data

`Content.Data` stores any data required for rendering within templates.

### Content Manager

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

*ContentManagerService* also stores URLs for commonly used paths such as the home page (*HomeURL*), login page (*AuthURL*), and error page (*ErrorURL*). These values can be changed from the defaults by your module and then the application can use your custom URLs for redirection. Methods for replacing the template paths in a *ContentManager* or adding to the existing paths are available.

*ContentManagerService* can also store the application name and a title value to be used, and it also contains an *ErrorHandler** function variable that allows you to override default ErrorHandler behavior.

See below for an example configuring `ContentManager`:

```go
import content_services "github.com/trentnix/hyperserver/pkg/services/content"

/* ... */

cm := content_services.NewContentManager()
cm.AddPageLayout("templates/some-layout.html")
cm.AddPageComponent("templates/componenents/some-component.html")

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
contentManager.AddPageComponent("templates/componenents/some-universal-component.html")
contentManager.AddHtmxComponent("templates/componenents/some-universal-component.html")
```

## ContentMessage Component

`ContentMessage`, defined in the **messages** package, provides session-based notifications stored via HTTP cookies. There are three types of `ContentMessage`:

- Success
- Error
- Default (informational)

There are functions available to make adding messages easy:

```go
messages.AddSuccessMessage(w, r, "action successful")
messages.AddErrorMessage(w, r, "error processing action")
messages.AddMessage(w, r, "this is just some useful information")
```

When you fetch messages from the session, they are cleared after retrieval and can't be fetched again. To fetch messages from the session:

```go
msgs, getMessagesErr := messages.GetMessages(r, w)
```

To create a new `ContentMessage` without adding it to a session:

```go
cm := messages.NewContentMessage("something bad happened", messages.MessageTypeError)
```

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
register.AddContent("templates/forms/register-form.html")
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
  register.AddContent("templates/forms/register-form.html")

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
        
        {{ if .HasErrors }}
            <div>{{ .GetFormErrorHTML }}</div>
        {{ else if .GetFormMessage }}
            <div>{{ .GetFormMessageHTML }}</div>
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