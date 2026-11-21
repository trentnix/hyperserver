# Developing content components

The blog owns components. Each component supplies its content rules, reader rendering, editor template, and editor script. The framework does not need to know component types.

Components stay in the Markdown revision for each post or page. The final `post-components` fence stores a versioned JSON envelope. A component can have a placement marker in the body. This boundary requires no database changes.

## Add a component

1. Add a definition and content type beside the component implementation. Use `modules/blog/audio_component.go` as an example.
2. Add its editor and picker icon under `modules/blog/templates/components/`. Add its script under `modules/blog/static/js/`.
3. Add the definition to `postComponentCatalog` in `modules/blog/component_catalog.go`.
4. Add validation, reader, and browser tests for its behavior.

The definition names the kind, form field, JSON payload key, editor template, icon, and script. It supplies the parser, reader renderer, and error for an added empty component. `Decode` is needed only when stored content differs from editor input, as with photo albums. `EditorData` can supply additional template data. `AliasFor` keeps an older stored kind editable under a canonical component choice and hides its picker entry. A submitted alias cannot accompany its canonical component.

Shared code discovers form fields, preserves rejected input, prepares templates, builds picker entries, and registers reader renderers from the catalog. The catalog also generates `/assets/js/component-editors.js` with ordinary module imports. Each editor loads before draft recovery initializes.

A component can supply a `FeedRenderer` for RSS. The feed uses the component's ordinary `Renderer` by default.

A component can supply `TransformDocument` when rendering requires the surrounding Markdown. The transformer runs after the component nodes are placed. Table of Contents uses it to collect headings and assign their link targets.

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

Content with post references supplies `PostReferences` for save validation and rendering. The application resolves public link metadata for all related-post components through `postLinkReader`. Pure Markdown rendering performs no lookup and omits unresolved links. See [related posts](components.md#related-posts).

## Editor contract

Use `initializeComponentPanels` to initialize sections when the page loads or when the editor adds another instance. Register each section with `registerComponent` and supply snapshot, clear, restore, focus, and normalization callbacks. Normalization defines which optional empty fields equal omitted fields. `jsonComponentValue` provides the shared JSON comparison.

Supply a `label` callback to identify an instance by its title, filename, or another detail in the editor. The editor adds a short reference when repeated instances need distinct labels.

Related posts and the Series section in the post editor share `post-picker.js` and the `blog.postPicker` partial. The picker owns search, selection, and movement. Its consumer owns validation, saving, and the serialized value. [Series](series.md) use shared relational metadata and do not belong to the component catalog.

The shared controller owns placement, removal, Undo, field errors, and unsaved indicators. It opens component settings in one dialog and keeps each component's fields mounted when its row moves. Goldmark supplies both the visible marker positions and valid destinations. Each instance has its own panel and form field. Use the panel as the scope for selectors and use the shared ID helpers for generated field IDs. Draft recovery includes the source for each active instance. A new component must not add another type switch or field list to those paths.

`TestComponentCatalogExtension` demonstrates a local component without changing the application catalog. Existing browser scenarios cover Save, rejected input, Undo, recovery, history, and narrow viewports.

## Saved formats

Components use the final top-level `post-components` fence. Version 2 stores an ordered `components` list. Version 3 adds standalone markers such as `<!-- hyperserver:component photo-album -->`. Version 4 adds optional instance IDs to the same list. Repeated instances store an `id` in the form `kind:<32 lowercase hex digits>`. Their markers use that ID. Earlier records use the component kind as their ID. Each marker renders its component at that position. Unplaced components render at the end in envelope order. Album-only posts can retain the original `photo-album` fence. Version 1 remains readable in its original album-then-downloads order. Unsupported versions remain visible as source and must be corrected before saving.

The [catalog](../../modules/blog/component_catalog.go) defines each kind, payload key, parser, and renderer. Component content types define JSON fields. Existing `x-post`, `bluesky-post`, `threads-post`, and `facebook-post` records retain their envelope keys and use the Social media editor. A unified Social media component can appear alongside those records.

Image and file references use shared media metadata and revision tables. Hidden images and playback files must retain references. Reader pages, feeds, and historical previews must enforce current publication and media-access rules. Search indexes human text rather than JSON fields and IDs. See [media storage](media.md#privacy-and-storage) and [backup compatibility](backup.md#compatibility).
