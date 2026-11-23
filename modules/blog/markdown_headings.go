package blog

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

type markdownHeadingIDs struct{}

func (markdownHeadingIDs) Transform(document *ast.Document, reader text.Reader, _ parser.Context) {
	used := make(map[string]bool)
	nextSuffix := make(map[string]int)
	for node := document.FirstChild(); node != nil; node = node.NextSibling() {
		heading, ok := node.(*ast.Heading)
		if !ok {
			continue
		}
		label := markdownHeadingLabel(heading, reader.Source())
		if label == "" {
			continue
		}
		words := strings.FieldsFunc(strings.ToLower(label), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsMark(r)
		})
		slug := strings.Join(words, "-")
		if slug == "" {
			slug = "heading"
		}
		base := "section-" + slug
		id := base
		for used[id] {
			nextSuffix[base]++
			id = base + "-" + strconv.Itoa(nextSuffix[base]+1)
		}
		used[id] = true
		heading.SetAttributeString("id", []byte(id))
	}
}

func markdownHeadingLabel(heading *ast.Heading, source []byte) string {
	var label strings.Builder
	ast.Walk(heading, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch value := node.(type) {
		case *ast.Text:
			text := value.Value(source)
			if value.Parent().Kind() != ast.KindCodeSpan {
				text = util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(text)))
			}
			label.Write(text)
			if value.HardLineBreak() || value.SoftLineBreak() {
				label.WriteByte(' ')
			}
		case *ast.String:
			label.Write(value.Value)
		case *ast.AutoLink:
			label.Write(value.Label(source))
		}
		return ast.WalkContinue, nil
	})
	return strings.Join(strings.Fields(label.String()), " ")
}
