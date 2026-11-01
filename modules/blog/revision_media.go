package blog

import (
	"context"
	"html/template"
	"io"
	"strings"

	"golang.org/x/net/html"
)

// revisionMissingImages checks both the body and preview without changing the snapshot.
func (m *module) revisionMissingImages(ctx context.Context, revision Revision, contentPath string) (map[string]bool, error) {
	refs, err := imageReferences(revision.Markdown, contentPath, revision.PreviewImage)
	if err != nil {
		return nil, err
	}
	return m.missingMedia(ctx, refs)
}

// markMissingImages replaces missing uploads in already-sanitized Markdown HTML.
// It preserves the stored source and never adds user-provided HTML.
func markMissingImages(rendered template.HTML, missing map[string]bool) (template.HTML, error) {
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
			if token.Data == "img" {
				for _, attr := range token.Attr {
					if attr.Key == "src" && missing[imageReference(attr.Val, "/")] {
						deleted = true
					}
				}
			}
			if deleted {
				out.WriteString(`<span class="missing-image">Image deleted</span>`)
			} else {
				out.WriteString(token.String())
			}
		default:
			out.Write(tokens.Raw())
		}
	}
}
