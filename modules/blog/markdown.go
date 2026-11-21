package blog

import (
	"bytes"
	"context"
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

func markdownComponents(source string, catalog componentCatalog, overrides ...renderer.NodeRenderer) (template.HTML, error) {
	var out bytes.Buffer
	var renderers []util.PrioritizedValue
	for index, definition := range catalog {
		renderers = append(renderers, util.Prioritized(definition.Renderer, 100+index))
	}
	for _, override := range overrides {
		renderers = append(renderers, util.Prioritized(override, 0))
	}
	transformers := []util.PrioritizedValue{util.Prioritized(componentsTransformer{catalog: catalog}, 110)}
	if _, supported := catalog.find("photo-album"); supported {
		transformers = append(transformers, util.Prioritized(albumTransformer{}, 100))
	}
	for index, definition := range catalog {
		if definition.TransformDocument != nil {
			transformers = append(transformers, util.Prioritized(definition.TransformDocument, 120+index))
		}
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

func (m *module) renderWriting(ctx context.Context, source, exclude string) (template.HTML, error) {
	return m.renderWritingWithCatalog(ctx, source, exclude, postComponentCatalog)
}

// Components can resolve public content references for this render. Pure Markdown
// rendering stays independent of storage for media extraction and other callers.
func (m *module) renderWritingWithCatalog(ctx context.Context, source, exclude string, catalog componentCatalog) (template.HTML, error) {
	_, components, err := catalog.split(source)
	if err != nil {
		return markdownComponents(source, catalog)
	}
	var readers []renderer.NodeRenderer
	if len(m.embedOrigins) > 0 {
		readers = append(readers, embedRenderer{origins: m.embedOrigins})
	}
	if ids := componentRelatedReferences(components); len(ids) > 0 {
		reader, err := m.resolveRelatedReader(ctx, exclude, ids)
		if err != nil {
			return "", err
		}
		readers = append(readers, reader)
	}
	return markdownComponents(source, catalog, readers...)
}

// markdownAt renders content for previews and feeds, resolving relative links
// and images against the content URL rather than the page displaying the preview.
// The shared Markdown renderer still removes raw HTML and unsafe URL schemes.
func markdownAt(source string, base *url.URL) (template.HTML, error) {
	rendered, err := markdown(source)
	if err != nil {
		return "", err
	}
	return prepareFeedHTML(rendered, base)
}

// prepareFeedHTML resolves feed links against the published post, including fragment-only links.
func prepareFeedHTML(rendered template.HTML, postURL *url.URL) (template.HTML, error) {
	return markdownURLs(rendered, postURL, false)
}

func (m *module) renderRevisionWriting(ctx context.Context, source, exclude string, base *url.URL) (template.HTML, error) {
	rendered, err := m.renderWriting(ctx, source, exclude)
	if err != nil {
		return "", err
	}
	return markdownURLs(rendered, base, true)
}

func markdownURLs(rendered template.HTML, base *url.URL, keepLocalFragments bool) (template.HTML, error) {
	tokens := html.NewTokenizer(strings.NewReader(string(rendered)))
	var out strings.Builder
	readerOnly := 0
	for {
		kind := tokens.Next()
		if readerOnly > 0 {
			if kind == html.StartTagToken && tokens.Token().Data == "div" {
				readerOnly++
			}
			if kind == html.EndTagToken && tokens.Token().Data == "div" {
				readerOnly--
			}
			if kind != html.ErrorToken {
				continue
			}
		}
		switch kind {
		case html.ErrorToken:
			if tokens.Err() == io.EOF {
				return template.HTML(out.String()), nil
			}
			return "", tokens.Err()
		case html.EndTagToken:
			token := tokens.Token()
			if token.Data != "iframe" && token.Data != "audio" {
				out.Write(tokens.Raw())
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokens.Token()
			if token.Data == "div" {
				for _, attr := range token.Attr {
					if attr.Key == "data-reader-only" {
						readerOnly = 1
						break
					}
				}
				if readerOnly > 0 {
					continue
				}
			}
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
				if keepLocalFragments && token.Data == "a" && strings.HasPrefix(attr.Val, "#") {
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
