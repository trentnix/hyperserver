package blog

import (
	"context"
	"errors"
	"fmt"
	"html"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

const maxRelatedLinks = 12
const relatedPagePrefix = "page:"

type postRelated struct {
	Title string   `json:"title,omitempty"`
	Posts []string `json:"posts,omitempty"` // Existing post-only components.
	Links []string `json:"links,omitempty"` // In order. Page IDs carry the page: prefix.
}

func relatedDefinition() componentDefinition {
	return componentDefinition{
		Kind: "related-posts", Field: "related", Payload: "related",
		Name: "Related content", Summary: "Links to other posts and pages", EmptyMessage: "Choose a post or page or remove the related content component.",
		TemplateFile: "templates/components/related.html", Template: "blog.relatedEditor", Icon: "blog.relatedIcon", Script: "related-editor.js",
		Parse: func(source string) (componentContent, error) { return parseRelated(source) }, Renderer: relatedRenderer{},
		EditorData: func(source string, _ bool) any {
			return postPickerData{ID: "post-related", Name: "related", Source: source, TitleLabel: "Heading", MaxPosts: maxRelatedLinks, OptionalTitle: true, IncludePages: true}
		},
	}
}

func parseRelated(source string) (*postRelated, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	var value *postRelated
	if err := decodeComponentJSON(source, &value); err != nil || value == nil || !utf8.ValidString(source) {
		return nil, contentError("Unable to read the related links. Check their source before saving.")
	}
	if value.Posts != nil && value.Links != nil {
		return nil, contentError("Use a single list of related links.")
	}
	refs := value.RelatedReferences()
	if len(refs) < 1 || len(refs) > maxRelatedLinks {
		return nil, contentError("Choose between 1 and 12 links.")
	}
	if utf8.RuneCountInString(value.Title) > 240 {
		return nil, contentError("Keep the related content heading within 240 characters.")
	}
	seen := make(map[string]bool, len(refs))
	for _, id := range refs {
		if !validRelatedLink(id) || value.Posts != nil && !validRelatedID(id) || seen[id] {
			return nil, contentError("Choose each post or page once using a valid ID.")
		}
		seen[id] = true
	}
	return value, nil
}

func validRelatedID(id string) bool {
	return len(id) == 26 && strings.Trim(id, "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567") == ""
}

func validRelatedLink(id string) bool {
	return validRelatedID(strings.TrimPrefix(id, relatedPagePrefix))
}

func relatedContentID(kind, id string) string {
	if kind == "page" && id != "" {
		return relatedPagePrefix + id
	}
	return id
}

func (value *postRelated) RelatedReferences() []string {
	if value.Links != nil {
		return value.Links
	}
	return value.Posts
}

func (value *postRelated) Empty() bool { return value == nil || len(value.RelatedReferences()) == 0 }
func (value *postRelated) EditorSource() string {
	if value == nil {
		return ""
	}
	return componentJSON(value)
}
func (value *postRelated) SearchText() string { return "\n" + value.Title }
func (*postRelated) Media() []componentMedia  { return nil }
func (value *postRelated) Node() ast.Node     { return &relatedNode{Related: *value} }

// Only public targets enter the request-owned renderer. Media extraction uses
// the pure renderer, which omits unresolved links without reading the database.
func (m *module) resolveRelatedReader(ctx context.Context, exclude string, ids []string) (renderer.NodeRenderer, error) {
	reader := relatedRenderer{links: make(map[string]relatedLink, len(ids))}
	var postIDs, pageIDs []string
	for _, id := range ids {
		if id == exclude {
			continue
		}
		if pageID, ok := strings.CutPrefix(id, relatedPagePrefix); ok {
			pageIDs = append(pageIDs, pageID)
		} else {
			postIDs = append(postIDs, id)
		}
	}
	for batch := range slices.Chunk(postIDs, maxRelatedLinks) {
		targets, err := m.posts.PublicPostLinks(ctx, batch)
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			reader.links[target.ID] = relatedLink{target.Title, "/post/s/" + target.Slug}
		}
	}
	for batch := range slices.Chunk(pageIDs, maxRelatedLinks) {
		targets, err := m.pageStore.PublicPageLinks(ctx, batch)
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			reader.links[relatedPagePrefix+target.ID] = relatedLink{target.Title, target.URL()}
		}
	}
	return reader, nil
}

type relatedNode struct {
	ast.BaseBlock
	Related postRelated
}

var relatedKind = ast.NewNodeKind("RelatedPosts")

func (*relatedNode) Kind() ast.NodeKind              { return relatedKind }
func (n *relatedNode) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type relatedLink struct{ title, path string }

type relatedRenderer struct{ links map[string]relatedLink }

func (reader relatedRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(relatedKind, func(writer util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		value := node.(*relatedNode).Related
		var links strings.Builder
		for _, id := range value.RelatedReferences() {
			if link, found := reader.links[id]; found {
				fmt.Fprintf(&links, `<li><a href="%s">%s</a></li>`, html.EscapeString(link.path), html.EscapeString(link.title))
			}
		}
		if links.Len() > 0 {
			title := strings.TrimSpace(value.Title)
			if title == "" {
				title = "Related content"
			}
			fmt.Fprintf(writer, `<nav class="related-posts" aria-label="%s"><h3>%s</h3><ul>%s</ul></nav>`, html.EscapeString(title), html.EscapeString(title), links.String())
		}
		return ast.WalkSkipChildren, nil
	})
}

func relatedReferences(source string) []string {
	_, components, _ := splitPostComponents(source)
	return componentRelatedReferences(components)
}

type relatedReferencesProvider interface{ RelatedReferences() []string }

func componentRelatedReferences(components postComponents) []string {
	var ids []string
	seen := make(map[string]bool)
	for _, component := range components.Components {
		if references, ok := component.Content.(relatedReferencesProvider); ok {
			for _, id := range references.RelatedReferences() {
				if !seen[id] {
					ids = append(ids, id)
					seen[id] = true
				}
			}
		}
	}
	return ids
}

// References in the current or selected revision survive unpublication and
// deletion. New references must point to public content or content the writer can edit.
func (m *module) validateRelatedReferences(ctx context.Context, source, kind, id string, revisionID int64) error {
	_, components, err := splitPostComponents(source)
	if err != nil {
		return writingFieldError{err, "content"}
	}
	var savedIDs []string
	loaded := false
	for _, component := range components.Components {
		references, ok := component.Content.(relatedReferencesProvider)
		if !ok {
			continue
		}
		if !loaded {
			savedIDs, err = m.currentRelatedReferences(ctx, kind, id)
			if err != nil {
				return err
			}
			loaded = true
		}
		for _, targetID := range references.RelatedReferences() {
			if targetID == relatedContentID(kind, id) {
				return writingFieldError{contentError("Related content cannot link back to the same post or page."), postComponentCatalog.field(component)}
			}
			if slices.Contains(savedIDs, targetID) {
				continue
			}
			available, err := m.canSelectRelatedLink(ctx, targetID)
			if err != nil {
				return err
			}
			if available {
				continue
			}
			if revisionID > 0 {
				previous, err := m.history.GetRevision(ctx, kind, id, revisionID)
				if err == nil {
					savedIDs = append(savedIDs, relatedReferences(previous.Markdown)...)
				} else if !errors.Is(err, errNotFound) {
					return err
				}
				revisionID = 0
			}
			if !slices.Contains(savedIDs, targetID) {
				return writingFieldError{contentError("A selected link is unavailable. Remove it or choose another post or page."), postComponentCatalog.field(component)}
			}
		}
	}
	return nil
}

func (m *module) currentRelatedReferences(ctx context.Context, kind, id string) ([]string, error) {
	if id == "" {
		return nil, nil
	}
	if kind == "post" {
		current, err := m.posts.GetByID(ctx, id, true)
		return relatedReferences(current.Markdown), err
	}
	current, err := m.pageStore.GetPageByID(ctx, id, true)
	return relatedReferences(current.Markdown), err
}
