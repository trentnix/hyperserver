# Rendering and forms

The [contact handler](../../modules/site/contact.go) demonstrates validation, persistence, and errors for both ordinary forms and HTMX.

## Prepare views once

Application setup configures shared layouts and response handlers before initializing modules. Each module prepares its own pages during initialization:

```go
view, err := content.PrepareView(cm, "home",
    content.TemplatePath{FS: templateFiles, Path: "templates/home.html"})
if err != nil {
    return err
}
```

Store the result on the module. Each request gets separate content and data:

```go
page := view.Content(r, data)
if err := page.Render(w, r); err != nil {
    cm.HandleError(w, r, "Unable to render the page", err, http.StatusInternalServerError)
}
```

`templateFiles` is an `fs.FS`, commonly an `embed.FS`. Paths are relative to that filesystem and use forward slashes. Layouts and page templates can use different filesystems.

The view binds the manager, prepared templates, and a named partial. Preparation checks that the partial exists in both the page and HTMX template sets. Layouts render it with `{{renderPartial .PartialName .}}`. Ordinary navigation and history restoration use the full-page layout. HTMX requests use the fragment layout.

To serve several partials from the same files, call `content.PrepareTemplates` once, then select each view without reparsing:

```go
listView, err := templates.View(cm, "notes.list")
if err != nil {
    return err
}
```

Use the same manager that prepared the templates. Nested `{{template "name" .}}` calls do not need view objects. Views share parsed templates, not rendered HTML or request data. `view.Content` returns the existing `*content.Content`, with response fields available for titles, redirects, and HTMX instructions. Direct `NewManagedContent` construction remains available when a handler chooses templates or partials per request.

Do not combine `Templates` with request-specific `AddLayout`, `AddContent`, or `AddComponent` calls. Those methods provide a per-render parsing path when no prepared set is supplied. Restart and prepare new sets to apply template changes. There is no file watcher.

Parsing errors and missing view partials stop initialization when returned by the module. Execution errors still occur per request. Rendering buffers all selected templates before writing response content. It does not undo application changes if rendering fails, and network writes can fail after partial delivery.

Run `go test ./pkg/components/content -run '^$' -bench '^BenchmarkRenderTemplates$' -benchmem` to compare prepared and per-render parsing. The benchmark excludes HTTP delivery and notification storage.

## Shared rendering policy

Configure `ContentManagerService` before serving. It selects shared page and HTMX templates, `HomeURL`, `AuthURL`, and response callbacks. `HandleMessage`, `HandleError`, and `HandleNotFound` render responses when called. Assigning them does not register routes.

The reference application's [rendering setup](../../cmd/web/modules.go) selects site defaults. Apply overrides afterward, before template preparation. `ApplyDefaults` rejects a second defaults policy. Explicit field and template overrides remain available.

`AddPage*` and `AddHtmx*` append template paths. `RegisterLayouts`, `RegisterContents`, and `RegisterComponents` replace the selected response type's paths. Calling a `Register*` method without filenames clears them.

Shared response handlers can install `ContentManagerService.PrepareTemplates`. `handlers.Setup` calls it before module initialization. Applications that bypass `Setup` must call the callback themselves before serving.

## HTMX responses

Set response instructions on the request's content:

```go
page.HTMX.Response = &htmx.Response{
    Trigger:  "saved",
    Retarget: "#editor",
    Reswap:   "outerHTML",
}
```

Other fields support after-swap and after-settle events, history URLs, and refreshes. These are server-selected instructions. Do not copy untrusted values into them. Ordinary and history-restoration requests ignore fragment response metadata.

For additional fragments, define named templates containing their own `hx-swap-oob` markup and set `page.OutOfBand = []string{"notice"}`. They share the main view's data and notifications. They render only for HTMX fragment requests, and all must succeed before response content is written.

| Intent | Setting | Result |
| --- | --- | --- |
| Redirect | `page.RedirectURL = "/saved"` | Ordinary 303 or HTMX 200 with `HX-Redirect`. |
| Send an HTMX event without a swap | `page.HTMX.Response = &htmx.Response{NoContent: true, Trigger: "saved"}` | HTMX 204 with the event. Ordinary requests still render. |
| Return no body in either mode | `page.ResponseStatusCode = http.StatusNoContent` | 204 without template execution. |
| Show validation errors | Render the form with errors | 200 by default so HTMX swaps the form. |

Redirects take precedence and skip templates and notification retrieval. Use `RedirectURL`, not a 3xx `ResponseStatusCode`. Store destination notifications explicitly. If an application chooses 422 for validation, it must configure HTMX to swap that status.

Rendered responses default to `Cache-Control: no-store` and add both HTMX request headers to `Vary`. An existing response `Cache-Control` header takes precedence over `page.Headers`. Keep sensitive responses uncacheable. Direct `http.Handler` and `http.ResponseWriter` remain available for streaming and downloads.

## Messages

`pkg/components/messages` defines message, notification, and system-message values without requiring sessions. Assign them directly to `Content.Messages`, `Content.Notifications`, or `Content.SystemMessages`.

For notifications across requests, use the [flash service](../services/flash/notifications.go) with session middleware:

```go
// During application setup:
contentManager.LoadNotifications = flash.LoadNotifications

// Before redirecting after a successful operation:
if err := flash.AddSuccessNotification(w, r, "Your settings were saved."); err != nil {
    // The operation succeeded, but its notification could not be stored.
    return err
}
```

A notification loader reads without removing messages. It returns an optional acknowledgment callback. Rendering acknowledges after all templates succeed and before writing headers. HEAD, redirects, refreshes, bodyless responses, and template failures preserve notifications. Loading or acknowledgment errors stop rendering. Templates must display the loaded notifications.

A nil loader permits direct notifications without session storage. `flash.LoadErrorNotifications` loads and removes only errors. Network failures and concurrent requests mean flash messages do not provide exactly-once delivery.

For categorized messages, `flash.AddSuccessMessage(w, r, text, category)` and `flash.GetMessages(w, r, category)` store and retrieve a group. Unlike notification loading, `GetMessages` removes the group immediately. HEAD leaves it unchanged. Check errors from both operations.

## Forms

Embed `form.Form` for errors and messages. Implement `Bind(*http.Request) error` to copy parsed inputs and `Validate() form.FieldErrors` for ordinary Go validation. Validation returns plain-text messages keyed by field name and must not change application data. See the [contact form](../../modules/site/contact.go) and [account rules](../../auth/modules/email/form_validation.go).

The POST handler stops on any parsing, binding, or validation error:

```go
contactForm := &ContactForm{}
page := contactView.Content(r, contactForm)

if err := form.ParseAndValidate(r, contactForm); err != nil {
    form.HandleFormError(w, r, page, contactForm, "Unable to process submission", err)
    return
}

// Save the validated submission and handle the operation's result.
```

A nil error means parsing, binding, and field validation succeeded. Invalid fields return `form.FieldErrors` and remain recorded for templates. `HandleFormError` renders those fields without logging or adding the generic message. Parser failures reject the request without rendering submitted values. Other errors are logged and show the supplied public message. Existing form-level messages remain unchanged by validation.

Parsing limits decoded fields to 16 KiB and encoded queries to 64 KiB, independently of the route body limit. `ParseAndValidateWithOptions` can change field limits or explicitly disable them. Account and contact forms return 413 for oversized input, 400 for malformed forms, and 415 for unsupported encodings.

Use both ordinary `method`/`action` attributes and HTMX attributes when a form must work without JavaScript. See the [contact template](../../modules/site/templates/html/partials/contact-form.html). Render submitted values and messages through `html/template`. For internationalized email addresses, use `type="text" inputmode="email"` and validate on the server.
