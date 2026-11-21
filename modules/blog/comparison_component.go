package blog

import (
	"fmt"
	"html"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

type postComparison struct {
	BeforeID    string `json:"beforeID"`
	AfterID     string `json:"afterID"`
	BeforeLabel string `json:"beforeLabel,omitempty"`
	AfterLabel  string `json:"afterLabel,omitempty"`
	BeforeAlt   string `json:"beforeAlt,omitempty"`
	AfterAlt    string `json:"afterAlt,omitempty"`
	Title       string `json:"title,omitempty"`
	Caption     string `json:"caption,omitempty"`
}

func comparisonDefinition() componentDefinition {
	return componentDefinition{
		Kind: "image-comparison", Field: "comparison", Payload: "comparison",
		Name: "Before & after", Summary: "Two images with a draggable comparison", EmptyMessage: "Choose both images or remove the before and after component.",
		TemplateFile: "templates/components/comparison.html", Template: "blog.comparisonEditor", Icon: "blog.comparisonIcon", Script: "comparison-editor.js",
		Parse: func(source string) (componentContent, error) { return parseComparison(source) }, Renderer: comparisonRenderer{},
		EditorData: func(data componentEditorData) any {
			return struct {
				componentEditorData
				Sides []string
			}{data, []string{"before", "after"}}
		},
	}
}

func parseComparison(source string) (*postComparison, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	var value *postComparison
	if err := decodeComponentJSON(source, &value); err != nil || value == nil || !utf8.ValidString(source) {
		return nil, contentError("Unable to read the image comparison. Check its source before saving.")
	}
	if !validImageID(value.BeforeID) || !validImageID(value.AfterID) {
		return nil, contentError("Choose an uploaded image for each side of the comparison.")
	}
	if value.BeforeID == value.AfterID {
		return nil, contentError("Choose two different images for the comparison.")
	}
	for _, field := range []struct {
		text  string
		limit int
	}{{value.Title, 240}, {value.Caption, 1000}, {value.BeforeAlt, 500}, {value.AfterAlt, 500}, {value.BeforeLabel, 80}, {value.AfterLabel, 80}} {
		if !utf8.ValidString(field.text) || utf8.RuneCountInString(field.text) > field.limit {
			return nil, contentError("Keep each label within 80 characters, the title within 240, the caption within 1,000, and each image description within 500.")
		}
	}
	return value, nil
}

func (value *postComparison) Empty() bool { return value == nil }
func (value *postComparison) EditorSource() string {
	if value.Empty() {
		return ""
	}
	return componentJSON(value)
}
func (value *postComparison) SearchText() string {
	return "\n" + value.Title + "\n" + value.Caption + "\n" + value.BeforeAlt + "\n" + value.AfterAlt + "\n" + value.BeforeLabel + "\n" + value.AfterLabel
}
func (value *postComparison) Media() []componentMedia {
	return []componentMedia{{ID: value.BeforeID}, {ID: value.AfterID}}
}
func (value *postComparison) Node() ast.Node { return &comparisonNode{Comparison: *value} }

type comparisonNode struct {
	ast.BaseBlock
	Comparison postComparison
}

var comparisonKind = ast.NewNodeKind("ImageComparison")

func (*comparisonNode) Kind() ast.NodeKind              { return comparisonKind }
func (n *comparisonNode) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type comparisonRenderer struct{}

func (comparisonRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(comparisonKind, func(writer util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		value := node.(*comparisonNode).Comparison
		beforeLabel, afterLabel := strings.TrimSpace(value.BeforeLabel), strings.TrimSpace(value.AfterLabel)
		if beforeLabel == "" {
			beforeLabel = "Before"
		}
		if afterLabel == "" {
			afterLabel = "After"
		}
		writer.WriteString(`<figure class="image-comparison" data-image-comparison aria-label="Image comparison">`)
		if value.Title != "" {
			fmt.Fprintf(writer, `<h3>%s</h3>`, html.EscapeString(value.Title))
		}
		writer.WriteString(`<div class="comparison-stage">`)
		for _, side := range []struct{ name, id, alt, label string }{{"before", value.BeforeID, value.BeforeAlt, beforeLabel}, {"after", value.AfterID, value.AfterAlt, afterLabel}} {
			alt := side.alt
			if alt == "" {
				alt = side.label
			}
			fmt.Fprintf(writer, `<div class="comparison-photo comparison-%s"><img src="/images/%s?variant=display" alt="%s" loading="lazy" decoding="async" draggable="false"><span class="comparison-label" title="%s">%s</span></div>`, side.name, side.id, html.EscapeString(alt), html.EscapeString(side.label), html.EscapeString(side.label))
		}
		fmt.Fprintf(writer, `<div data-reader-only class="comparison-divider" hidden><div class="comparison-handle"><input type="range" min="0" max="100" value="50" aria-label="Image comparison" aria-valuetext="%s"><span aria-hidden="true"><svg viewBox="0 0 24 24"><path d="m8 7-5 5 5 5m8-10 5 5-5 5"/></svg></span></div></div></div>`, html.EscapeString("50% "+beforeLabel+", 50% "+afterLabel))
		if value.Caption != "" {
			fmt.Fprintf(writer, `<figcaption>%s</figcaption>`, html.EscapeString(value.Caption))
		}
		writer.WriteString(`</figure>`)
		return ast.WalkSkipChildren, nil
	})
}
