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

## Messages

Each Content instance has three different message types:

- Notifications
- ContentMessage
- SystemMessage

The **messages** package defines each message type, with methods that allow messages of each type to be stored and retrieved.

### ContentMessage Component

`ContentMessage`, defined in the **messages** package, provides session-stored messages earmarked for rendering inside of content. There are three types of `ContentMessage`:

- Success
- Error
- Default (informational)

There are functions available to make adding content messages easy:

```go
messages.AddSuccessMessage(w, r, "processing complete")
messages.AddErrorMessage(w, r, "try a different login mechanism")
messages.AddMessage(w, r, "processing took 2 minutes and 15 seconds")
```

When you fetch messages from the session, they are cleared after retrieval and can't be fetched again. To fetch content messages from the session:

```go
msgs, getMessagesErr := messages.GetContentMessages(r, w)
```

To create a new `ContentMessage` without adding it to a session:

```go
cm := messages.NewContentMessage("you have a new update", messages.MessageTypeError)
```

### Notifications

A `Notification`, defined in the **messages** package, provides session-stored messages earmarked for notifying a user of the success or failure of some action. There are two types of `Notification` messages:

- Success
- Error

There are functions available to make adding notifications easy:

```go
messages.AddSuccessNotification(w, r, "logout successful")
messages.AddErrorNotification(w, r, "unable to access the specified resource")
```

Like content messages, when you fetch notifications from the session they are cleared after retrieval and can't be fetched again. To fetch notifications from the session:

```go
msgs, getMessagesErr := messages.GetNotifications(r, w)
```

To create a new `Notification` without adding it to a session:

```go
cm := messages.NewNotification("something bad happened", messages.NotificationTypeError)
```

### System Messages

A `SystemMessage`, defined in the **messages** package, provides session-stored messages earmarked for logging or console messages (if the requestor is a browser). There are several types of `SystemMessage` messages:

- Debug
- Log
- Info
- Warning
- Error

There are functions available to make adding `SystemMessage` instances easy:

```go
messages.AddSystemDebugMessage(w, r, "entered the function")
messages.AddSystemLogMessage(w, r, "processed callback")
messages.AddSystemInfoMessage(w, r, "database is version 2.7")
messages.AddSystemWarningMessage(w, r, "callback processing exceed 200ms")
messages.AddSystemErrorMessage(w, r, "call to server failed")
```

Like content messages and notifications, when you fetch system messages from the session they are cleared after retrieval and can't be fetched again. To fetch system messages from the session:

```go
msgs, getMessagesErr := messages.GetSystemMessages(r, w)
```

To create a new `SystemMessage` without adding it to a session:

```go
cm := messages.NewSystemMessage("something bad happened", messages.SystemMessageTypeError)
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
        
        {{ if .HasErrorMessages }}
            <div class="form-messages">{{ .GetErrorMessagesHTML }}</div>
        {{ else if .HasInfoMessages }}
            <div class="form-messages">{{ .GetInfoMessagesHTML }}</div>
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