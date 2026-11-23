package blog

import (
	"fmt"
	"html"
	"slices"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

type postTableOfContents struct {
	Levels []int `json:"levels,omitempty"`
}

func tableOfContentsDefinition() componentDefinition {
	return componentDefinition{
		Kind: "table-of-contents", Field: "tableOfContents", Payload: "tableOfContents",
		Name: "Table of Contents", Summary: "Jump links to headings", EmptyMessage: "Add the table of contents again or remove it.",
		TemplateFile: "templates/components/table-of-contents.html", Template: "blog.tableOfContentsEditor",
		Icon: "blog.tableOfContentsIcon", Script: "table-of-contents-editor.js",
		Parse:             parseTableOfContents,
		Renderer:          tableOfContentsRenderer{},
		TransformDocument: tableOfContentsTransformer{},
	}
}

func parseTableOfContents(source string) (componentContent, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	value := &postTableOfContents{Levels: []int{2, 3}}
	if err := decodeComponentJSON(source, &value); err != nil || value == nil {
		return nil, contentError("Unable to read the table of contents. Check its source or remove it.")
	}
	if len(value.Levels) == 0 {
		return nil, contentError("Choose at least one heading level.")
	}
	slices.Sort(value.Levels)
	for i, level := range value.Levels {
		if level < 1 || level > 6 || i > 0 && level == value.Levels[i-1] {
			return nil, contentError("Choose each heading level from H1 through H6 at most once.")
		}
	}
	if slices.Equal(value.Levels, []int{2, 3}) {
		value.Levels = nil
	}
	return value, nil
}

func (p *postTableOfContents) Empty() bool           { return p == nil }
func (p *postTableOfContents) EditorSource() string  { return componentJSON(p) }
func (*postTableOfContents) SearchText() string      { return "" }
func (*postTableOfContents) Media() []componentMedia { return nil }
func (p *postTableOfContents) Node() ast.Node {
	var levels [7]bool
	if p.Levels == nil {
		levels[2], levels[3] = true, true
	}
	for _, level := range p.Levels {
		levels[level] = true
	}
	return &tableOfContentsNode{levels: levels}
}

type tableOfContentsHeading struct {
	label, id string
	level     int
}

type tableOfContentsEntry struct {
	tableOfContentsHeading
	children []*tableOfContentsEntry
}

type tableOfContentsNode struct {
	ast.BaseBlock
	levels  [7]bool
	entries []*tableOfContentsEntry
}

var tableOfContentsKind = ast.NewNodeKind("TableOfContents")

func (*tableOfContentsNode) Kind() ast.NodeKind { return tableOfContentsKind }
func (n *tableOfContentsNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

type tableOfContentsTransformer struct{}

func (tableOfContentsTransformer) Transform(document *ast.Document, reader text.Reader, _ parser.Context) {
	var outlines []*tableOfContentsNode
	for node := document.FirstChild(); node != nil; node = node.NextSibling() {
		if outline, ok := node.(*tableOfContentsNode); ok {
			outlines = append(outlines, outline)
		}
	}
	if len(outlines) == 0 {
		return
	}

	var headings []tableOfContentsHeading
	for node := document.FirstChild(); node != nil; node = node.NextSibling() {
		heading, ok := node.(*ast.Heading)
		if !ok {
			continue
		}
		attribute, _ := heading.AttributeString("id")
		if id, ok := attribute.([]byte); ok {
			headings = append(headings, tableOfContentsHeading{
				label: markdownHeadingLabel(heading, reader.Source()), id: string(id), level: heading.Level,
			})
		}
	}

	entriesByLevels := make(map[[7]bool][]*tableOfContentsEntry)
	for _, outline := range outlines {
		if _, ok := entriesByLevels[outline.levels]; !ok {
			entriesByLevels[outline.levels] = tableOfContentsEntries(headings, outline.levels)
		}
		outline.entries = entriesByLevels[outline.levels]
	}
}

func tableOfContentsEntries(headings []tableOfContentsHeading, levels [7]bool) []*tableOfContentsEntry {
	var entries []*tableOfContentsEntry
	var parents []*tableOfContentsEntry
	total := 0
	for _, heading := range headings {
		// Even an omitted heading ends the scope of an earlier section at
		// the same level or deeper.
		for len(parents) > 0 && parents[len(parents)-1].level >= heading.level {
			parents = parents[:len(parents)-1]
		}
		if !levels[heading.level] {
			continue
		}
		entry := &tableOfContentsEntry{tableOfContentsHeading: heading}
		if len(parents) == 0 {
			entries = append(entries, entry)
		} else {
			parent := parents[len(parents)-1]
			parent.children = append(parent.children, entry)
		}
		parents = append(parents, entry)
		total++
	}
	if total < 2 {
		return nil
	}
	return entries
}

type tableOfContentsRenderer struct{}

func (tableOfContentsRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(tableOfContentsKind, func(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		outline := node.(*tableOfContentsNode)
		if len(outline.entries) > 0 {
			w.WriteString(`<nav class="post-toc" aria-label="On this page" hx-boost="false"><strong>On this page</strong>`)
			renderTableOfContentsList(w, outline.entries)
			w.WriteString("</nav>\n")
		}
		return ast.WalkSkipChildren, nil
	})
}

func renderTableOfContentsList(w util.BufWriter, entries []*tableOfContentsEntry) {
	w.WriteString("<ul>")
	for _, entry := range entries {
		fmt.Fprintf(w, `<li><a href="#%s">%s</a>`, html.EscapeString(entry.id), html.EscapeString(entry.label))
		if len(entry.children) > 0 {
			renderTableOfContentsList(w, entry.children)
		}
		w.WriteString("</li>")
	}
	w.WriteString("</ul>")
}
