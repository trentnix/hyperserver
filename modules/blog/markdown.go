package blog

import (
	"bytes"
	"html/template"
	"io"
	"net/url"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
	"golang.org/x/net/html"
)

const maxMarkdownBytes = 256 * 1024

// markdown is the trust boundary for rendered blog content. Goldmark omits raw
// HTML and unsafe URL schemes. Previews and published pages use the same renderer.
func markdown(source string) (template.HTML, error) {
	return markdownComponents(source, postComponentCatalog)
}

func markdownComponents(source string, catalog componentCatalog) (template.HTML, error) {
	var out bytes.Buffer
	var renderers []util.PrioritizedValue
	for index, definition := range catalog {
		renderers = append(renderers, util.Prioritized(definition.Renderer, 100+index))
	}
	transformers := []util.PrioritizedValue{util.Prioritized(componentsTransformer{catalog: catalog}, 110)}
	if _, supported := catalog.find("photo-album"); supported {
		transformers = append(transformers, util.Prioritized(albumTransformer{}, 100))
	}
	engine := goldmark.New(
		goldmark.WithParserOptions(parser.WithASTTransformers(transformers...)),
		goldmark.WithRendererOptions(renderer.WithNodeRenderers(renderers...)),
	)
	if err := engine.Convert([]byte(source), &out); err != nil {
		return "", err
	}
	return template.HTML(out.String()), nil
}

// markdownAt renders content for previews and feeds, resolving relative links
// and images against the content URL rather than the page displaying the preview.
// The shared Markdown renderer still removes raw HTML and unsafe URL schemes.
func markdownAt(source string, base *url.URL) (template.HTML, error) {
	rendered, err := markdown(source)
	if err != nil {
		return "", err
	}
	tokens := html.NewTokenizer(strings.NewReader(string(rendered)))
	var out strings.Builder
	for {
		switch tokens.Next() {
		case html.ErrorToken:
			if tokens.Err() == io.EOF {
				return template.HTML(out.String()), nil
			}
			return "", tokens.Err()
		case html.EndTagToken:
			if tokens.Token().Data != "iframe" && tokens.Token().Data != "audio" {
				out.Write(tokens.Raw())
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokens.Token()
			if token.Data == "audio" {
				continue // The audio fallback link works in feed readers without media controls.
			}
			if token.Data == "iframe" {
				for _, attr := range token.Attr {
					if attr.Key != "src" {
						continue
					}
					if video, err := videoURL(attr.Val); err == nil {
						link := html.Token{Type: html.StartTagToken, Data: "a", Attr: []html.Attribute{{Key: "href", Val: video.viewingURL()}}}
						out.WriteString(link.String() + "Open on " + video.Provider + "</a>")
					}
				}
				continue
			}
			for i, attr := range token.Attr {
				if attr.Val == "" || !(token.Data == "a" && attr.Key == "href" || token.Data == "img" && attr.Key == "src") {
					continue
				}
				ref, err := url.Parse(attr.Val)
				if err == nil && !ref.IsAbs() {
					token.Attr[i].Val = base.ResolveReference(ref).String()
				}
			}
			out.WriteString(token.String())
		default:
			out.Write(tokens.Raw())
		}
	}
}
