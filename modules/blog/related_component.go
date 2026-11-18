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

const maxRelatedPosts = 12

type postRelated struct {
	Title string   `json:"title,omitempty"`
	Posts []string `json:"posts"`
}

func relatedDefinition() componentDefinition {
	return componentDefinition{
		Kind: "related-posts", Field: "related", Payload: "related",
		Name: "Related posts", Summary: "Links to other posts", EmptyMessage: "Choose a post or remove the related posts component.",
		TemplateFile: "templates/components/related.html", Template: "blog.relatedEditor", Icon: "blog.relatedIcon", Script: "related-editor.js",
		Parse: func(source string) (componentContent, error) { return parseRelated(source) }, Renderer: relatedRenderer{},
		EditorData: func(source string, _ bool) any {
			return postPickerData{ID: "post-related", Name: "related", Source: source, TitleLabel: "Heading", MaxPosts: maxRelatedPosts, OptionalTitle: true}
		},
	}
}

func parseRelated(source string) (*postRelated, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	var value *postRelated
	if err := decodeComponentJSON(source, &value); err != nil || value == nil || !utf8.ValidString(source) {
		return nil, contentError("Unable to read the related posts. Check their source before saving.")
	}
	if len(value.Posts) < 1 || len(value.Posts) > maxRelatedPosts {
		return nil, contentError("Choose between 1 and 12 posts.")
	}
	if utf8.RuneCountInString(value.Title) > 240 {
		return nil, contentError("Keep the related posts heading within 240 characters.")
	}
	seen := make(map[string]bool, len(value.Posts))
	for _, id := range value.Posts {
		if !validRelatedID(id) || seen[id] {
			return nil, contentError("Choose each post only once using a valid post ID.")
		}
		seen[id] = true
	}
	return value, nil
}

func validRelatedID(id string) bool {
	return len(id) == 26 && strings.Trim(id, "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567") == ""
}

func (value *postRelated) Empty() bool { return value == nil || len(value.Posts) == 0 }
func (value *postRelated) EditorSource() string {
	if value == nil {
		return ""
	}
	return componentJSON(value)
}
func (value *postRelated) SearchText() string       { return "\n" + value.Title }
func (*postRelated) Media() []componentMedia        { return nil }
func (value *postRelated) Node() ast.Node           { return &relatedNode{Related: *value} }
func (value *postRelated) PostReferences() []string { return value.Posts }

// Only public targets enter the request-owned renderer. Media extraction uses
// the pure renderer, which omits unresolved links without reading the database.
func (value *postRelated) ResolveReader(ctx context.Context, posts postLinkReader, postID string) (renderer.NodeRenderer, error) {
	targets, err := posts.PublicPostLinks(ctx, value.Posts)
	if err != nil {
		return nil, err
	}
	reader := relatedRenderer{posts: make(map[string]Post, len(targets)), exclude: postID}
	for _, target := range targets {
		reader.posts[target.ID] = target
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

type relatedRenderer struct {
	posts   map[string]Post
	exclude string
}

func (reader relatedRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(relatedKind, func(writer util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		value := node.(*relatedNode).Related
		var links strings.Builder
		for _, id := range value.Posts {
			post, found := reader.posts[id]
			if !found || id == reader.exclude {
				continue
			}
			fmt.Fprintf(&links, `<li><a href="/post/s/%s">%s</a></li>`, html.EscapeString(post.Slug), html.EscapeString(post.Title))
		}
		if links.Len() > 0 {
			title := strings.TrimSpace(value.Title)
			if title == "" {
				title = "Related posts"
			}
			fmt.Fprintf(writer, `<nav class="related-posts" aria-label="%s"><h3>%s</h3><ul>%s</ul></nav>`, html.EscapeString(title), html.EscapeString(title), links.String())
		}
		return ast.WalkSkipChildren, nil
	})
}

func postReferences(source string) []string {
	_, components, _ := splitPostComponents(source)
	var ids []string
	for _, component := range components.Components {
		if references, ok := component.Content.(interface{ PostReferences() []string }); ok {
			ids = append(ids, references.PostReferences()...)
		}
	}
	return ids
}

// Existing references survive unpublication and deletion. New references must
// point to public content or private content the author can edit.
func (m *module) validatePostReferences(ctx context.Context, post Post) error {
	_, components, err := splitPostComponents(post.Markdown)
	if err != nil {
		return writingFieldError{err, "content"}
	}
	var previous []string
	if post.ID != "" && len(postReferences(post.Markdown)) > 0 {
		current, err := m.posts.GetByID(ctx, post.ID, true)
		if err != nil {
			return err
		}
		previous = postReferences(current.Markdown)
	}
	for _, component := range components.Components {
		references, ok := component.Content.(interface{ PostReferences() []string })
		if !ok {
			continue
		}
		definition, _ := postComponentCatalog.find(component.Type)
		for _, id := range references.PostReferences() {
			if id == post.ID {
				return writingFieldError{contentError("A post cannot link to itself in related posts."), definition.Field}
			}
			if slices.Contains(previous, id) {
				continue
			}
			_, err := m.posts.GetByID(ctx, id, false)
			if err == nil {
				continue
			}
			if !errors.Is(err, errNotFound) {
				return err
			}
			target, err := m.posts.GetByID(ctx, id, true)
			if err != nil && !errors.Is(err, errNotFound) {
				return err
			}
			if err != nil || !m.canEditPost(ctx, target) {
				return writingFieldError{contentError("A selected post is unavailable. Remove it or choose another post."), definition.Field}
			}
		}
	}
	return nil
}
