# Contact-file storage

The [file provider](file_contacts.go) is a sample alternative to SQLite. Select it with `go run ./cmd/web -contact-directory ./tmp/contacts`. It creates one file per submission. It has no read, list, update, delete, or conversion API.

## Stored format

Each published file is named `<Id>.json`. The provider generates a UUID for `Id` and a creation timestamp for `CreatedAt`, replacing any values supplied by the caller. The file contains one UTF-8 JSON object followed by a newline:

```json
{"Id":"92464e33-fd8b-47bd-b25e-c15a931893e4","Name":"Example visitor","Email":"visitor@example.invalid","Message":"Hello","CreatedAt":"2026-10-14T12:00:00Z"}
```

The five field names are case-sensitive parts of the stored format. `Id`, `Name`, `Email`, and `Message` are strings. `CreatedAt` uses Go's `time.Time` JSON representation: an RFC 3339 timestamp with a timezone and optional fractional seconds. Readers must not assume UTC or fixed timestamp precision. Empty `Name` and `Message` values remain present. JSON escaping and object-key order are not application data.

Files have no format-version field or envelope. Consumers can decode the object into [ContactSubmission](../models/contact_submissions.go) using `encoding/json`. A [fixed fixture](testdata/contact-unversioned.json) and [format tests](file_format_test.go) protect the documented field names, values, and timestamp representation.

## Compatibility and publication

Opening the repository checks directory access and hard-link support. It does not read, validate, convert, or repair existing documents. Creating a submission publishes a complete file without overwriting an existing path. Existing files remain unchanged, including files the current format cannot interpret.

No conversion is currently supported or required. A future incompatible format must have an explicit discriminator and a documented reader or conversion path for existing documents. Any conversion must test preservation of the original data on failure and interruption before being supported. Conversion belongs to this provider or its consuming application, not a framework-wide file migration service.

Keep the directory private to the application. New directories use mode `0700`, and new files use `0600`, subject to the process umask. Setup leaves existing directory permissions unchanged. Temporary files use `.contact-*` names and are not published records. A crash can leave temporary files behind. Successful publication does not guarantee power-loss durability because the provider does not sync files or directory entries to disk.
