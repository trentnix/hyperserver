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

type postCallout struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body"`
	Style string `json:"style,omitempty"`
}

func calloutDefinition() componentDefinition {
	return componentDefinition{
		Kind: "callout", Field: "callout", Payload: "callout",
		Name: "Callout", Summary: "Highlight a note in the post", EmptyMessage: "Enter callout text or remove the component.",
		TemplateFile: "templates/components/callout.html", Template: "blog.calloutEditor", Icon: "blog.calloutIcon", Script: "markdown-component-editor.js",
		Parse:    func(source string) (componentContent, error) { return parseCallout(source) },
		Renderer: calloutRenderer{},
	}
}

func parseCallout(source string) (*postCallout, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	var callout *postCallout
	if err := decodeComponentJSON(source, &callout); err != nil || callout == nil || !utf8.ValidString(source) {
		return nil, contentError("Unable to read the callout. Check its source before saving.")
	}
	callout.Title = strings.TrimSpace(callout.Title)
	if utf8.RuneCountInString(callout.Title) > 240 {
		return nil, contentError("Use a callout heading of at most 240 characters.")
	}
	if strings.TrimSpace(callout.Body) == "" || utf8.RuneCountInString(callout.Body) > 20000 {
		return nil, contentError("Enter callout text of at most 20,000 characters.")
	}
	switch callout.Style {
	case "", "slate", "caution", "success":
		return callout, nil
	default:
		return nil, contentError("Choose an available callout style.")
	}
}

func (c *postCallout) Empty() bool             { return c == nil }
func (c *postCallout) EditorSource() string    { return componentJSON(c) }
func (c *postCallout) SearchText() string      { return "\n" + c.Title + "\n" + c.Body }
func (c *postCallout) Media() []componentMedia { return nil }
func (c *postCallout) Node() ast.Node          { return &calloutNode{Callout: *c} }

type calloutNode struct {
	ast.BaseBlock
	Callout postCallout
}

var calloutKind = ast.NewNodeKind("Callout")

func (*calloutNode) Kind() ast.NodeKind              { return calloutKind }
func (n *calloutNode) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type calloutRenderer struct{}

func (calloutRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(calloutKind, func(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		callout := node.(*calloutNode).Callout
		w.WriteString(`<div class="post-callout" role="note"`)
		if callout.Style != "" {
			fmt.Fprintf(w, ` data-callout-style="%s"`, html.EscapeString(callout.Style))
		}
		w.WriteString(`>`)
		if callout.Title != "" {
			fmt.Fprintf(w, `<p class="post-callout-heading"><strong>%s</strong></p>`, html.EscapeString(callout.Title))
		}
		if err := goldmark.New().Convert([]byte(callout.Body), w); err != nil {
			return ast.WalkStop, err
		}
		w.WriteString(`</div>`)
		return ast.WalkSkipChildren, nil
	})
}
