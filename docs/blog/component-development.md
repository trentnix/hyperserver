# Developing post components

The blog owns components. Each component supplies its content rules, reader rendering, editor template, and editor script. The framework does not need to know component types.

Components stay in the post's Markdown revision. The final `post-components` fence stores a versioned JSON envelope in display order. The reader also accepts the original photo-album fence and version 1 envelope. This boundary requires no database changes.

## Add a component

1. Add a definition and content type beside the component implementation. Use `modules/blog/audio_component.go` as an example.
2. Add its editor and picker icon under `modules/blog/templates/components/`. Add its script under `modules/blog/static/js/`.
3. Add the definition to `postComponentCatalog` in `modules/blog/component_catalog.go`.
4. Add validation, reader, and browser tests for its behavior.

The definition names the kind, form field, JSON payload key, editor template, icon, and script. It supplies the parser, reader renderer, and error for an added empty component. `Decode` is needed only when stored content differs from editor input, as with photo albums. `EditorData` can supply additional template data. `AliasFor` keeps an older stored kind editable under a canonical component choice and hides its picker entry. A submitted alias cannot accompany its canonical component.

Shared code discovers form fields, preserves rejected input, prepares templates, builds picker entries, and registers reader renderers from the catalog. The catalog also generates `/assets/js/component-editors.js` with ordinary module imports. Each editor loads before draft recovery initializes.

A component can select a provider internally, as [Social media](social-posts.md) does from its URL. Legacy definitions preserve existing envelopes without adding picker entries.

## Content contract

A component's parser must reject unsupported fields and invalid content. An added empty component must fail Save. The author must fill it or remove it.

The content type supplies:

- `Empty`: whether the component has content.
- `EditorSource`: the editable source for saved content.
- `SearchText`: human text to include in body search.
- `Media`: every referenced upload, including hidden images or download links. Include `Bytes` when Save must verify a file's recorded size.
- `Node`: the Goldmark node handled by the component's reader renderer.

Reader rendering must escape authored text. Raw HTML remains disabled in Markdown. Media authorization, storage, quotas, and cleanup stay shared.

Content with post references can supply `PostReferences` for save validation and `ResolveReader` for a request-owned renderer. The narrow `postLinkReader` contract resolves public link metadata. The application renderer handles reader pages, previews, feeds, and history. Pure Markdown rendering performs no lookup and omits unresolved links. See [related posts](components.md#related-posts).

## Editor contract

Each script registers its section with `registerComponent`. It supplies snapshot, clear, restore, focus, and normalization callbacks. Normalization defines which optional empty fields equal omitted fields. `jsonComponentValue` provides the shared JSON comparison.

Related posts and the Series section in the post editor share `post-picker.js` and the `blog.postPicker` partial. The picker owns search, selection, and movement. Its consumer owns validation, saving, and the serialized value. [Series](series.md) use shared relational metadata and do not belong to the component catalog.

Reordering preserves iframe state when the browser supports `moveBefore`. Editors with live previews can supply `beforeMove` and `afterMove` callbacks to release and rebuild their preview when an older browser must remove and reinsert the section.

The shared controller owns ordering, removal, Undo, field errors, and unsaved indicators. Draft recovery discovers component source fields from the page. A new component must not add another type switch or field list to those paths.

`TestComponentCatalogExtension` demonstrates a local component without changing the application catalog. Existing browser scenarios cover Save, rejected input, Undo, recovery, history, and narrow viewports.

## Saved formats

Components use the final top-level `post-components` fence. Version 2 stores an ordered `components` list. Album-only posts can retain the original `photo-album` fence. Version 1 remains readable in its original album-then-downloads order. Unsupported versions remain visible as source and must be corrected before saving.

The [catalog](../../modules/blog/component_catalog.go) defines each kind, payload key, parser, and renderer. Component content types define JSON fields. Existing `x-post`, `bluesky-post`, `threads-post`, and `facebook-post` records retain their envelope keys and use the Social media editor. Older envelopes containing multiple provider-specific records remain readable. A new canonical component cannot accompany an alias for that component.

Image and file references use shared media metadata and revision tables. Hidden images and playback files must retain references. Reader pages, feeds, and historical previews must enforce current publication and media-access rules. Search indexes human text rather than JSON fields and IDs. See [media storage](media.md#privacy-and-storage) and [backup compatibility](backup.md#compatibility).
