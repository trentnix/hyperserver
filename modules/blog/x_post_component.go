package blog

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

var xPostID = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var xUsername = regexp.MustCompile(`^[A-Za-z0-9_]{1,15}$`)

// Keep only a post ID. Share parameters, usernames, and media suffixes never
// become embed markup or network destinations supplied by the author.
func xPostURL(raw string) (string, error) {
	invalid := contentError("Enter an X post URL, such as https://x.com/username/status/123456789.")
	u, err := linkURL(raw)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(u.Hostname()) {
	case "x.com", "www.x.com", "mobile.x.com", "twitter.com", "www.twitter.com", "mobile.twitter.com":
	default:
		return "", invalid
	}
	if u.RawPath != "" {
		return "", invalid
	}
	parts := strings.Split(strings.TrimSuffix(u.Path, "/"), "/")
	// /user/status/id and /i/web/status/id are both public share-link forms.
	if len(parts) >= 5 && parts[1] == "i" && parts[2] == "web" {
		parts = append([]string{"", "i"}, parts[3:]...)
	}
	if len(parts) != 4 && len(parts) != 6 {
		return "", invalid
	}
	if !xUsername.MatchString(parts[1]) || parts[2] != "status" || !xPostID.MatchString(parts[3]) {
		return "", invalid
	}
	if len(parts) == 6 && ((parts[4] != "photo" && parts[4] != "video") || (len(parts[5]) != 1 || parts[5][0] < '1' || parts[5][0] > '4')) {
		return "", invalid
	}
	return parts[3], nil
}

type xPostNode struct {
	ast.BaseBlock
	Post postSocialPost
}

var xPostKind = ast.NewNodeKind("XPost")

func (*xPostNode) Kind() ast.NodeKind              { return xPostKind }
func (n *xPostNode) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type xPostRenderer struct{}

func (xPostRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(xPostKind, func(writer util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		post := node.(*xPostNode).Post
		id, _ := xPostURL(post.URL) // The parser validates before creating the node.
		writer.WriteString(`<section class="x-post-component" aria-label="X post">`)
		if post.Description != "" {
			fmt.Fprintf(writer, `<p class="x-post-description">%s</p>`, html.EscapeString(post.Description))
		}
		writer.WriteString(`<div class="x-post-media">`)
		if post.ImageID != "" {
			fmt.Fprintf(writer, `<a class="x-post-fallback" data-x-post-fallback href="https://x.com/i/web/status/%s" target="_blank" rel="noopener noreferrer" hx-boost="false"><img src="/images/%s?variant=display" alt="Screenshot of the X post" loading="lazy" decoding="async"></a>`, id, post.ImageID)
		}
		fmt.Fprintf(writer, `<div data-reader-only class="x-post-embed" data-x-post="%s"></div></div><div class="x-post-actions"><div data-reader-only><button type="button" class="button-primary" data-x-post-load hidden>Load X post</button></div><a href="https://x.com/i/web/status/%s" target="_blank" rel="noopener noreferrer" hx-boost="false">Open on X</a></div>`, id, id)
		writer.WriteString(`<div data-reader-only><p class="x-post-status" data-x-post-status role="status"></p></div></section>`)
		return ast.WalkSkipChildren, nil
	})
}
