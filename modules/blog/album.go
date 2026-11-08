package blog

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const maxAlbumPhotos = 50

// Albums remain part of the writing, so existing revisions, backups, image
// permissions, and cleanup use the same content and reference transaction.
var albumLine = regexp.MustCompile(`^!\[((?:\\.|[^\]\\])*)\]\(/images/([a-f0-9]{64}\.(?:png|jpeg|gif))\)$`)

type albumPhoto struct{ ID, Caption string }

func parseAlbum(source string) ([]albumPhoto, error) {
	var photos []albumPhoto
	seen := make(map[string]bool)
	for line := range strings.SplitSeq(strings.ReplaceAll(source, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		match := albumLine.FindStringSubmatch(line)
		if match == nil {
			return nil, contentError("Choose uploaded images for the album, one Markdown image per line.")
		}
		caption := unescapeAlbumCaption(match[1])
		if !utf8.ValidString(match[1]) || utf8.RuneCountInString(caption) > 500 {
			return nil, contentError("Keep each album caption under 500 characters.")
		}
		if seen[match[2]] {
			return nil, contentError("Each photo can appear only once in the album.")
		}
		seen[match[2]] = true
		photos = append(photos, albumPhoto{match[2], caption})
		if len(photos) > maxAlbumPhotos {
			return nil, contentError("Choose at most 50 photos for the album.")
		}
	}
	return photos, nil
}

func unescapeAlbumCaption(value string) string {
	var result strings.Builder
	escaped := false
	for _, character := range value {
		if character == '\\' && !escaped {
			escaped = true
			continue
		}
		result.WriteRune(character)
		escaped = false
	}
	return result.String()
}

// finalAlbumBlock is the format boundary shared by editing and rendering.
// Earlier blocks, nested code, and blockquotes remain ordinary writing.
func finalAlbumBlock(document ast.Node, source []byte) *ast.FencedCodeBlock {
	block, ok := document.LastChild().(*ast.FencedCodeBlock)
	if !ok || string(block.Language(source)) != "photo-album" {
		return nil
	}
	return block
}

func splitPostAlbum(source string) (string, string) {
	data := []byte(source)
	document := goldmark.New().Parser().Parse(text.NewReader(data))
	block := finalAlbumBlock(document, data)
	if block == nil {
		return source, ""
	}
	start := strings.LastIndex(source[:block.Info.Segment.Start], "\n") + 1
	return strings.TrimRight(source[:start], "\r\n"), string(block.Lines().Value(data))
}

func joinPostAlbum(body, source string) (string, error) {
	photos, err := parseAlbum(source)
	if err != nil {
		return body, err
	}
	if len(photos) == 0 {
		return body, nil
	}
	combined := strings.TrimRight(body, "\r\n") + "\n\n```photo-album\n" + strings.TrimSpace(source) + "\n```\n"
	_, appended := splitPostAlbum(combined)
	if strings.TrimSpace(appended) != strings.TrimSpace(source) {
		return body, writingFieldError{contentError("Close any unfinished code or HTML blocks before adding a photo album."), "content"}
	}
	return combined, nil
}

func (p editorPage) BodyMarkdown() string {
	body, _ := splitPostAlbum(p.Markdown)
	return body
}

func (p pageContent) BodyMarkdown() string { return p.Markdown }

func (p editorPage) AlbumMarkdown() string {
	if p.AlbumInput != nil {
		return *p.AlbumInput
	}
	_, album := splitPostAlbum(p.Markdown)
	return album
}

var albumKind = ast.NewNodeKind("PhotoAlbum")

type albumNode struct {
	ast.BaseBlock
	Photos []albumPhoto
}

func (*albumNode) Kind() ast.NodeKind              { return albumKind }
func (n *albumNode) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type albumTransformer struct{}

func (albumTransformer) Transform(document *ast.Document, reader text.Reader, _ parser.Context) {
	block := finalAlbumBlock(document, reader.Source())
	if block == nil {
		return
	}
	photos, err := parseAlbum(string(block.Lines().Value(reader.Source())))
	if err != nil || len(photos) == 0 {
		return
	}
	document.ReplaceChild(document, block, &albumNode{Photos: photos})
}

type albumRenderer struct{}

func (albumRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(albumKind, func(writer util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		// Only fixed markup and escaped captions cross the Markdown trust boundary.
		writer.WriteString(`<section class="photo-album" aria-label="Photo album"><h2>Photos</h2><div class="photo-album-grid">`)
		for index, photo := range node.(*albumNode).Photos {
			caption := photo.Caption
			if caption == "" {
				caption = fmt.Sprintf("Photo %d", index+1)
			}
			alt := html.EscapeString(caption)
			caption = html.EscapeString(photo.Caption)
			path := "/images/" + photo.ID // parseAlbum accepts only validated upload IDs.
			fmt.Fprintf(writer, `<a href="%s" data-album-photo data-caption="%s" hx-boost="false"><figure><img src="%s?variant=thumbnail" alt="%s" loading="lazy" decoding="async">`, path, caption, path, alt)
			if photo.Caption != "" {
				fmt.Fprintf(writer, `<figcaption>%s</figcaption>`, caption)
			}
			writer.WriteString(`</figure></a>`)
		}
		writer.WriteString(`</div></section>`)
		return ast.WalkSkipChildren, nil
	})
}
