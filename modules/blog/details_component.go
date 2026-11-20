package blog

import (
	"fmt"
	"html"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

type postDetails struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func detailsDefinition() componentDefinition {
	return componentDefinition{
		Kind: "expandable-details", Field: "details", Payload: "details",
		Name: "Expandable details", Summary: "Markdown readers can expand", EmptyMessage: "Enter a title and details or remove the component.",
		TemplateFile: "templates/components/details.html", Template: "blog.detailsEditor", Icon: "blog.detailsIcon", Script: "markdown-component-editor.js",
		Parse:        func(source string) (componentContent, error) { return parseDetails(source) },
		Renderer:     detailsRenderer{},
		FeedRenderer: detailsRenderer{expanded: true},
	}
}

func parseDetails(source string) (*postDetails, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	var details *postDetails
	if err := decodeComponentJSON(source, &details); err != nil || details == nil || !utf8.ValidString(source) {
		return nil, contentError("Unable to read the expandable details. Check its source before saving.")
	}
	details.Title = strings.TrimSpace(details.Title)
	if details.Title == "" || utf8.RuneCountInString(details.Title) > 240 {
		return nil, contentError("Enter a title of at most 240 characters for the expandable details.")
	}
	if strings.TrimSpace(details.Body) == "" || utf8.RuneCountInString(details.Body) > 20000 {
		return nil, contentError("Enter details of at most 20,000 characters.")
	}
	return details, nil
}

func (d *postDetails) Empty() bool             { return d == nil }
func (d *postDetails) EditorSource() string    { return componentJSON(d) }
func (d *postDetails) SearchText() string      { return "\n" + d.Title + "\n" + d.Body }
func (d *postDetails) Media() []componentMedia { return nil }
func (d *postDetails) Node() ast.Node          { return &detailsNode{Details: *d} }

type detailsNode struct {
	ast.BaseBlock
	Details postDetails
}

var detailsKind = ast.NewNodeKind("ExpandableDetails")

func (*detailsNode) Kind() ast.NodeKind              { return detailsKind }
func (n *detailsNode) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type detailsRenderer struct{ expanded bool }

func (r detailsRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(detailsKind, func(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		details := node.(*detailsNode).Details
		title := html.EscapeString(details.Title)
		if r.expanded {
			fmt.Fprintf(w, `<div><p><strong>%s</strong></p>`, title)
		} else {
			fmt.Fprintf(w, `<details class="expandable-details"><summary>%s</summary><div class="expandable-details-body">`, title)
		}
		// Plain Markdown keeps nested component fences as code and omits raw HTML.
		if err := goldmark.New().Convert([]byte(details.Body), w); err != nil {
			return ast.WalkStop, err
		}
		w.WriteString(`</div>`)
		if !r.expanded {
			w.WriteString(`</details>`)
		}
		return ast.WalkSkipChildren, nil
	})
}
