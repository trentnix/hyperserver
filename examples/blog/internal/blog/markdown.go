package blog

import (
	"bytes"
	"html/template"
	"io"
	"net/url"
	"strings"

	"github.com/yuin/goldmark"
	"golang.org/x/net/html"
)

const maxMarkdownBytes = 256 * 1024

// markdown is the trust boundary for rendered blog content. Goldmark omits raw
// HTML and unsafe URL schemes. Previews and published pages use the same renderer.
func markdown(source string) (template.HTML, error) {
	var out bytes.Buffer
	if err := goldmark.New().Convert([]byte(source), &out); err != nil {
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
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokens.Token()
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
