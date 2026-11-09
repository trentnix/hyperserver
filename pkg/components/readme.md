# Rendering and forms

The test-only [contact handler](../../internal/test/web/contact_fixture_test.go) demonstrates validation, persistence, and errors for both ordinary forms and HTMX.

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
    page.HandleRenderError(w, r, "Unable to render the page")
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

Use the same manager that prepared the templates. Nested `{{template "name" .}}` calls do not need view objects. Views share parsed templates, not rendered HTML or request data. `view.Content` returns a request-local `*content.Content` for titles, redirects, and HTMX instructions. Direct `NewManagedContent` construction also accepts prepared templates.

Do not combine `Templates` with request-specific `AddLayout`, `AddContent`, or `AddComponent` calls. Those methods provide a per-render parsing path when no prepared set is supplied. Restart and prepare new sets to apply template changes. There is no file watcher.

Return preparation errors from `Init` to stop startup. Execution errors still occur per request. Rendering buffers all templates before writing content. Use `page.HandleRenderError` to report failures. It avoids writing another response after a delivery failure. Rendering failures do not undo application changes.

Run `go test ./pkg/components/content -run '^$' -bench '^BenchmarkRenderTemplates$' -benchmem` to compare prepared and per-render parsing. The benchmark excludes HTTP delivery and notification storage.

## Shared rendering policy

Configure `ContentManagerService` before serving. It selects shared page and HTMX templates, `HomeURL`, `AuthURL`, and response callbacks. `HandleMessage`, `HandleError`, and `HandleNotFound` render responses when called. Assigning them does not register routes.

The blog's [rendering setup](../../modules/blog/presentation.go) selects application defaults. Apply overrides afterward, before template preparation. `ApplyDefaults` rejects a second defaults policy. Explicit field and template overrides remain available.

`AddPage*` and `AddHtmx*` append template paths. `RegisterLayouts`, `RegisterContents`, and `RegisterComponents` replace the selected response type's paths. Calling a `Register*` method without filenames clears them.

Shared response handlers can install `ContentManagerService.PrepareTemplates`. `handlers.Setup` calls it before module initialization. Applications that bypass `Setup` must call the callback themselves before serving.

For standard HTML messages, errors, and not-found responses, select named partials:

```go
err := content.ConfigureResponses(cm, content.ResponseTemplates{
    Message:         "message",
    Error:           "error",
    NotFound:        "not-found",
    NotFoundMessage: "That page could not be found.",
}, content.TemplatePath{FS: templateFiles, Path: "templates/responses.html"})
```

Return configuration errors to startup. Each partial receives the public message as `.Data`. All three names are required and can select the same partial. The helper installs the preparation callback and three handlers. It rejects an existing preparation callback. Layout overrides apply before startup parsing, and explicit handler overrides remain available afterward.

These responses use `no-store`, set the HTML content type, and discard any preexisting content length. Error causes are logged, not rendered. Error and not-found responses skip notification storage. Rendering failures produce a plain-text 500 without recursively invoking the error handler. Delivery failures are logged without attempting another response. Module-specific form errors and ordinary views continue to use the existing rendering API.

## HTMX responses

The bundled browser integration uses HTMX 4. Parent attributes must opt into inheritance, for example `hx-target:inherited="#content"` and `hx-boost:inherited="true"`. Request events expose `event.detail.ctx`. Use `htmx:after:swap` for completed DOM updates and `htmx:finally:request` for request cleanup. Requests with `HX-Request-Type: full`, including `hx-select` requests, receive the full-page layout.

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

Redirects take precedence and skip templates and notification retrieval. Use `RedirectURL`, not a 3xx `ResponseStatusCode`. Store destination notifications explicitly. If an application chooses 422 for validation, it must enable error swaps, for example with the script below.

Rendered responses default to `Cache-Control: no-store` and add `HX-Request`, `HX-History-Restore-Request`, and `HX-Request-Type` to `Vary`. An existing response `Cache-Control` header takes precedence over `page.Headers`. Keep sensitive responses uncacheable. Direct `http.Handler` and `http.ResponseWriter` remain available for streaming and downloads.

### Browser errors

The optional framework script handles HTTP and connection errors without application-specific JavaScript. Register it once in the application or a module:

```go
routes.HandleFunc("GET /js/htmx-errors.js", htmx.ServeErrorScript)
```

Load it once after HTMX in the page layout:

```html
<script src="/js/htmx-errors.js" defer></script>
<div data-request-errors></div>
```

HTML 4xx/5xx responses swap into the selected target, including validation fragments. Plain-text errors appear as text without replacing inputs. Other response types receive a generic status message. Connection failures warn that a submitted change may have completed. The script does not retry requests or turn HTTP failures into successful HTMX events.

Alerts appear in the submitting form or dialog, then the target's form or dialog. Otherwise, they use the optional `[data-request-errors]` container or the document body. Style `[data-request-error]` in the application stylesheet. Application handlers still own public error messages and validation markup. Only send HTML errors intended for the selected target.

The script supports HTMX 4 and removes script elements from swapped fragments. Keep scripts in the page layout and enforce a Content Security Policy.

Sending a new request clears that source's previous alert. A failed retry displays a new alert. Canceled requests and requests from other controls leave existing alerts alone. Programmatic requests without a source use their target to distinguish alerts.

## Messages

`pkg/components/messages` defines message, notification, and system-message values without requiring sessions. Assign them directly to `Content.Messages`, `Content.Notifications`, or `Content.SystemMessages`.

For notifications across requests, use the [flash service](../services/flash/notifications.go) with session middleware:

```go
// During application setup:
contentManager.LoadNotifications = flash.LoadNotifications

// Before redirecting after a successful operation:
if err := flash.AddSuccessNotification(w, r, "Your settings were saved."); err != nil {
    // The operation succeeded, but its notification could not be stored.
    logger.LogRequestError(r, err)
}
```

A notification loader reads without removing messages. It returns an optional acknowledgment callback. Rendering acknowledges after all templates succeed and before writing headers. HEAD, redirects, refreshes, bodyless responses, and template failures preserve notifications. Loading or acknowledgment errors stop rendering. Templates must display the loaded notifications.

A nil loader permits direct notifications without session storage. `flash.LoadErrorNotifications` loads and removes only errors. Network failures and concurrent requests mean flash messages do not provide exactly-once delivery.

For categorized messages, `flash.AddSuccessMessage(w, r, text, category)` and `flash.GetMessages(w, r, category)` store and retrieve a group. Unlike notification loading, `GetMessages` removes the group immediately. HEAD leaves it unchanged. Check errors from both operations.

## Forms

Embed `form.Form` for errors and messages. Implement `Bind(*http.Request) error` to copy parsed inputs and `Validate() form.FieldErrors` for ordinary Go validation. Validation returns plain-text messages keyed by field name and must not change application data. See the [contact form](../../internal/test/web/contact_fixture_test.go) and [account rules](../../auth/modules/email/form_validation.go).

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

If rendering fails, `HandleFormError` uses `ContentManager.HandleError`, or a plain-text 500 response when no handler is configured.

Parsing limits decoded fields to 16 KiB and encoded queries to 64 KiB, independently of the route body limit. `ParseAndValidateWithOptions` can change field limits or explicitly disable them. Account and contact forms return 413 for oversized input, 400 for malformed forms, and 415 for unsupported encodings.

Use both ordinary `method`/`action` attributes and HTMX attributes when a form must work without JavaScript. See the [contact template](../../internal/test/web/testdata/templates/partials/contact-form.html). Render submitted values and messages through `html/template`. For internationalized email addresses, use `type="text" inputmode="email"` and validate on the server.
