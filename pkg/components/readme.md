# HyperServer Components

HyperServer is a framework to build HyperMedia applications with Go. To help facilitate serving HyperMedia to requestors, a set of components are implemented to abstract away some of the complexity of serving hypertext-based content. The Components that have been implemented are found in the *components* folder and are implemented:

- *Content* - *Content* is a view model that renders the HTML templates and accompanying data.
- *ContentMessage* - A *ContentMessage* is a means of communicating messages to the requestor.
- *Form* - A *Form* is an interface that defines interactions that configures, manages, and validates forms that are rendered and form data.
- *htmx.Request* - An *htmx.Request* object stores HTMX-related request attributes.
- *htmx.Response* - An *htmx.Response* object stores HTMX-related response attributes and provides a way to configure HTMX responses.

This document will attempt to dive into each of these components, explain how they can be used, and explain some of the design decisions I made.

## content Package

The *Content* struct is a view model defined in the **content** package to render a webpage or HTMX response. It stores metadata about a web page, such as Site, Title, and URL.

### Template Paths

*Content* stores the paths to the various templates that are used to render a page. These layouts are divided into three categories:

- Layouts
- Contents
- Components

**Layouts** are intended to store layout template paths that serve as the scaffolding for rendered content. For a web page would traditionally include the header, body, navbar, etc. **Layouts** are ignored in the event that Content is being served via an HTMX reponse, since only a partial page is being returned.

**Contents** stores template paths for the primary content that is being rendered to the requestor.

**Components** stores template paths for common content that might be rendered via a **Layout** or a **Content** template.

To create a *Content* instance and render it to the user, you might do something like this:

```go
homepage := content.NewContent(httpRequest /* *http.Request */)
homepage.Title = "Home Page"
homepage.AddLayout("templates/layout.html")
homepage.AddContent("templates/home-content.html")
homepage.AddComponent("templates/footer.html")

err := homepage.Render(w, r)
if err != nil {
  http.Error(w, "there was an error rendering the home page", http.StatusInternalServerError)
}
```

If there are multiple templates, each *Add* method includes a plural version that can take multiple templates:

```go
homepage := content.NewContent(httpRequest /* *http.Request */)
homepage.Title = "Home Page"
homepage.AddLayout("templates/layout.html", "templates/admin-layout.html")
homepage.AddContents("templates/home-content.html", "templates/admin-content.html")
homepage.AddComponents("templates/footer.html", "templates/admin-components.html")

err := homepage.Render(w, r)
if err != nil {
  http.Error(w, "there was an error rendering the home page", http.StatusInternalServerError)
}
```

In both examples, if the request is an HTMX request, the *Layout* values will actually be ignored.

### Content.Data

The *Data* member of a *Content* struct can store whatever data you need to render in the templates that are used.

### Content Manager

The application creates a singleton instance of a *ContentManager* that manager template paths that can be used across the application. The application's *ContentManager* can be accessed via the *GetContentManager* function in the **content** package. Alternatively, a new *ContentManager* can be created via *NewContentManager* if, for some reason, the application's instance won't cut it.

The *ContentManager* stores *Layouts*, *Contents*, and *Components* template paths that can be used throughout the application. These can be accessed directly or a *ContentManager* instance can be added to a *Content* instance:

```go
homepage := content.NewContent(httpRequest /* *http.Request */)
homepage.ContentManager = content.GetContentManager()
```

Alternatively, a *Content* instance can be created with the *ContentManager* already set:

```go
homepage := content.NewManagedContent(content.GetContentManager())
```

*ContentManager* also stores URLs for commonly used paths such as the home page (*HomeURL*), login page (*AuthURL*), and error page (*ErrorURL*). These values can be changed from the defaults by your module and then the application can use your custom URLs for redirection. Methods for replacing the template paths in a *ContentManager* or adding to the existing paths are available.

*ContentManager* can also store the application name and a title value to be used, and it also contains an *ErrorHandler** function variable that allows you to override default ErrorHandler behavior.

See below for an example of how to configure a *ContentManager* when initializing a custom module:

```go
func (m *SiteModule) Init(s *server.ApplicationServer) error {
  contentManager := content.GetContentManager()
  contentManager.AddPageLayout("templates/some-layout.html")
  contentManager.AddPageComponent("templates/componenents/some-component.html")

  contentManager.HomeURL = homeURL
  contentManager.AuthURL = authURL

  contentManager.ErrorHandler = m.RedirectToError

return nil
}

func (m *SiteModule) RedirectToError(w http.ResponseWriter, r *http.Request, message string, e error) {
  http.Error(w, fmt.Sprintf("%s - %v", message, e), http.StatusInternalServerError)
}
```

In the previous example, the *ContentManager* has a *Layout* and a *Component* added. But the methods used are *AddPageLayout* and *AddPageComponent*. These methods add template paths that use the **PageType**, so they will render if the request is a non-HTMX request.

If a *Component*, for example, needed to be added for use only in HTMX requests, you would do the following:

```go
contentManager.AddHtmxComponent("templates/componenents/htmx/some-htmx-component.html")
```

When rendering, *Content* will detect whether the request was an HTMX request or not and use the *ContentManager* templates that match the circumstance. If you need the same template path rendered in both cases, add a template path with both PageType and HtmxType:

```go
contentManager.AddPageComponent("templates/componenents/some-universal-component.html")
contentManager.AddHtmxComponent("templates/componenents/some-universal-component.html")
```

## messages Package

A *ContentMessage* is a structure defined in the **messages** package that contains a string message (accessed via *ContentMessage.Message*) and a message type (accessed via *ContentMessage.MessageType*). These messages can be added anywhere that has access to the parameters of a *HandlerFunc*, as they are stored in a session (and, consequently, some or all data is stored in a HTTP cookie).

### ContentMessage Types

There are three types of messages:

- Success
- Error
- Default

There are methods to add each kind of message to the session, abstracting away the details of how these types are maintained. To add a success message, simply call *AddSuccessMessage*:

```go
messages.AddSuccessMessage(w /* http.ResponseWriter */, r /* *http.Request */, "your action was successful")
```

And you can similarly call *AddErrorMessage* and *AddMessage* for errors and informational messages, respectively.

### Create a New ContentMessage

If you need to create a new *ContentMessage* for some other use, call *NewContentMessage*:

```go
cm := messages.NewContentMessage("something bad happened", messages.MessageTypeError)
```

### Retrieving Content Messages

When *ContentMessages* are added to a Session, they can be retrieved by using *GetMessages*. However, retrieving the messages stored in the current session will result in the messages being removed. So a subsequent *GetMessages* call would return an empty *ContentMessage* slice.

```go
msgs, getMessagesErr := messages.GetMessages(r, w)
```

If you need to configure the session store used for *ContentMessage* instances, set a store value for sessions with the name "hs-message-session". Otherwise, the default session store will be used (unless it is not defined, and then none of this *ContentMessage* session storage will work!).

## form Package

The *Form* struct implements the *FormComponent* interface and provides a simple way to create new forms, validate form values, and report form errors.

See the example of a user registration form below:

```go
RegisterForm struct {
  Email         string `validate:"required,email"`
  Password      string `validate:"required,password"`
  PasswordMatch string `validate:"required,password,eqfield=Password"`
  form.Form
}
```

### Rendering a Form

Each field in the form has annotation to specify the validation that should be done on the field. The inclusion of form.Form ensures that the FormComponent interface is satisfied.

To render a form, add a template path for the form and set the *Content.Data* to an instance of the form:

```go
register := content.NewManagedContent(r)
register.AddContent("templates/forms/register-form.html")
register.Data = &RegisterForm{}

err := register.Render(w, r)
if err != nil {
  /* deal with the error */
}
```

### Validate a Form

To validate a form, use the *form.Validate* method. It takes any struct that implements the *FormComponent* interface and uses popular validation package [Validator V10](https://github.com/go-playground/validator):

```go
register := content.NewManagedContent(r)
register.AddContent("templates/forms/register-form.html")
registerForm := &RegisterForm{}

r.ParseForm();
registerForm.Email = r.FormValue("email")
registerForm.Password = r.FormValue("password")
registerForm.PasswordMatch = r.FormValue("passwordMatch")

err := form.Validate(registerForm)
if err != nil {
    content.HandleFormError(w, r, register, registerForm, "The registration form could not be validated")
    return
}
```

*HandleFormError* is a special error handler that will re-render the form with error messages. 

### Form HTML Template Example

An example of the form template can be seen below:

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
        <span class="field-error">{{.}}</p>
    {{- end}}
{{end}}
```

## htmx Package

The **htmx** package contains structs and functions that intend to make it easy to use and manipulate HTMX requests and responses. One example is the *IsHtmxRequest* function. It takes a \*http.Request and will return `true` if the request is an HTMX request.

### HTMX Request

A *Request* struct is defined in the **htmx** package and contains the various attributes you'd expect on an HTMX request. A *htmx.Request* instance can be retrieved via the *htmx.GetRequest* function and is extracted from the \*http.Request parameter:

```go
htmxRequest := htmx.GetRequest(r /* *http.Request */)
```

Then any HTMX request attributes will be conveniently available.

### HTMX Reponse

A *Reponse* struct is defined in the **htmx** package and can be instantiated via the *htmx.NewResponse* function. A number of HTMX response fields (that will be used by the client-side HTMX JS library to control rendering) are then available and can be set.

For example, the *Response.TriggerAfterSwap* value can be changed to trigger client-side events after the swap step.