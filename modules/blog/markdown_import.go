package blog

import (
	"bytes"
	"errors"
	"html"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// markdownImportImage replaces a byte range with Before, an uploaded image URL,
// and After. The browser selects and uploads the file named by Path.
type markdownImportImage struct {
	Start  int    `json:"start"`
	End    int    `json:"end"`
	Path   string `json:"path"`
	Before string `json:"before"`
	After  string `json:"after"`
}

func (m *module) prepareMarkdownImport(w http.ResponseWriter, r *http.Request) {
	source, err := io.ReadAll(r.Body)
	var limit *http.MaxBytesError
	switch {
	case errors.As(err, &limit):
		writeJSON(w, r, http.StatusRequestEntityTooLarge, map[string]string{"error": "Choose a Markdown file of at most 256 KiB."})
		return
	case err != nil:
		writeJSON(w, r, http.StatusBadRequest, map[string]string{"error": "Unable to read the Markdown file."})
		return
	case !utf8.Valid(source) || bytes.IndexByte(source, 0) >= 0:
		writeJSON(w, r, http.StatusUnprocessableEntity, map[string]string{"error": "Choose a Markdown file encoded as UTF-8."})
		return
	case len(bytes.TrimSpace(source)) == 0:
		writeJSON(w, r, http.StatusUnprocessableEntity, map[string]string{"error": "The Markdown file is empty."})
		return
	}
	images, err := markdownImportImages(source)
	if err != nil {
		writeJSON(w, r, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, r, http.StatusOK, images)
}

func markdownImportImages(source []byte) ([]markdownImportImage, error) {
	linkParser := &importImageParser{
		InlineParser: parser.NewLinkParser(),
		images:       make(map[*ast.Image]markdownImportImage),
	}
	inline := parser.DefaultInlineParsers()
	for i := range inline {
		if inline[i].Value == linkParser.InlineParser {
			inline[i].Value = linkParser
			break
		}
	}
	document := parser.NewParser(
		parser.WithBlockParsers(parser.DefaultBlockParsers()...),
		parser.WithInlineParsers(inline...),
		parser.WithParagraphTransformers(parser.DefaultParagraphTransformers()...),
	).Parse(text.NewReader(source))

	if finalComponentsBlock(document, source) != nil || finalAlbumBlock(document, source) != nil {
		return nil, contentError("Remove the blog component data before importing this Markdown.")
	}
	for node := document.FirstChild(); node != nil; node = node.NextSibling() {
		if componentMarker(node, source) != "" {
			return nil, contentError("Remove the blog component placement markers before importing this Markdown.")
		}
	}
	images := make([]markdownImportImage, 0)
	next := 0
	err := ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if image, ok := node.(*ast.Image); entering && ok {
			if edit, ok := linkParser.images[image]; ok {
				if edit.Start < next || edit.End < edit.Start || edit.End > len(source) {
					return ast.WalkStop, contentError("Unable to locate a local image in the Markdown file.")
				}
				images = append(images, edit)
				next = edit.End
			}
			// An image nested inside alt text does not render a separate image.
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return images, err
}

// Goldmark exposes an image's opening offset and destination. Capture the
// destination range while its link parser still has the closing offset.
type importImageParser struct {
	parser.InlineParser
	images map[*ast.Image]markdownImportImage
}

func (p *importImageParser) Parse(parent ast.Node, reader text.Reader, context parser.Context) ast.Node {
	line, start := reader.Position()
	node := p.InlineParser.Parse(parent, reader, context)
	image, ok := node.(*ast.Image)
	if !ok {
		return node
	}
	destination, err := url.Parse(string(util.URLEscape(image.Destination, true)))
	if err != nil || destination.IsAbs() || destination.Host != "" || destination.Path == "" || strings.HasPrefix(destination.Path, "/") {
		return node
	}
	filename := path.Clean(destination.Path)
	if strings.HasSuffix(destination.Path, "/") {
		filename += "/"
	}
	suffix := (&url.URL{RawQuery: destination.RawQuery, ForceQuery: destination.ForceQuery, Fragment: destination.Fragment, RawFragment: destination.RawFragment}).String()
	edit := markdownImportImage{Path: filename, Before: "<", After: html.EscapeString(suffix) + ">"}
	endLine, end := reader.Position()
	if image.Reference != nil {
		edit.Start, edit.End = start.Start+1, end.Start
		edit.Before = "(<"
		if len(image.Title) > 0 {
			edit.After += ` "` + importImageTitle(image.Title) + `"`
		}
		edit.After += ")"
	} else {
		reader.SetPosition(line, start)
		reader.Advance(2) // Skip the label's closing bracket and opening parenthesis.
		reader.SkipSpaces()
		_, position := reader.Position()
		edit.Start = position.Start
		edit.End = edit.Start + len(image.Destination)
		if reader.Peek() == '<' {
			edit.End += 2
		}
		reader.SetPosition(endLine, end)
	}
	p.images[image] = edit
	return node
}

// Reference titles move into inline Markdown. Escape the decoded title so it
// keeps its meaning regardless of the reference definition's delimiters.
func importImageTitle(title []byte) string {
	title = util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(title)))
	return strings.NewReplacer("\\", "&#92;", "\r", "&#13;", "\n", "&#10;").Replace(html.EscapeString(string(title)))
}
