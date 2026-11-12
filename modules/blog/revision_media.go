package blog

import (
	"context"
	"html/template"
	"io"
	"strings"

	"golang.org/x/net/html"
)

// revisionMissingMedia checks both the body and preview without changing the snapshot.
func (m *module) revisionMissingMedia(ctx context.Context, revision Revision, contentPath string) (map[string]bool, error) {
	refs, err := contentMediaReferences(revision.Markdown, contentPath, revision.PreviewImage)
	if err != nil {
		return nil, err
	}
	return m.missingMedia(ctx, refs)
}

// markMissingMedia replaces missing uploads in already-sanitized Markdown HTML.
// It preserves the stored source and never adds user-provided HTML.
func markMissingMedia(rendered template.HTML, missing map[string]bool) (template.HTML, error) {
	if len(missing) == 0 {
		return rendered, nil
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
			deleted := false
			if token.Data == "img" || token.Data == "a" {
				for _, attr := range token.Attr {
					if (attr.Key == "src" || attr.Key == "href") && missing[mediaReferenceURL(attr.Val, "/")] {
						deleted = true
					}
				}
			}
			if deleted {
				if token.Data == "img" {
					out.WriteString(`<span class="missing-image">Image deleted</span>`)
				} else {
					for i, attr := range token.Attr {
						if attr.Key == "href" {
							token.Attr = append(token.Attr[:i], token.Attr[i+1:]...)
							break
						}
					}
					token.Attr = append(token.Attr, html.Attribute{Key: "aria-disabled", Val: "true"}, html.Attribute{Key: "title", Val: "File deleted"})
					out.WriteString(token.String())
				}
			} else {
				out.WriteString(token.String())
			}
		default:
			out.Write(tokens.Raw())
		}
	}
}
