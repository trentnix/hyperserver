package blog

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

type postXPost struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
	ImageID     string `json:"imageID,omitempty"`
}

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

func xPostDefinition() componentDefinition {
	return componentDefinition{
		Kind: "x-post", Field: "xPost", Payload: "xPost",
		Name: "X post", Summary: "A public post and its media", EmptyMessage: "Enter an X post URL or remove the X post component.",
		TemplateFile: "templates/components/x-post.html", Template: "blog.xPostEditor", Icon: "blog.xPostIcon", Script: "x-post-editor.js",
		Parse: func(source string) (componentContent, error) { return parseXPost(source) }, Renderer: xPostRenderer{},
	}
}

func parseXPost(source string) (*postXPost, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	var value *postXPost
	if err := decodeComponentJSON(source, &value); err != nil || value == nil || !utf8.ValidString(source) {
		return nil, contentError("Unable to read the X post. Check its source before saving.")
	}
	if _, err := xPostURL(value.URL); err != nil {
		return nil, err
	}
	if !utf8.ValidString(value.Description) || utf8.RuneCountInString(value.Description) > 1000 {
		return nil, contentError("Keep the X post description at most 1,000 characters.")
	}
	if value.ImageID != "" && !validImageID(value.ImageID) {
		return nil, contentError("Choose an uploaded image for the X post fallback.")
	}
	return value, nil
}

func (value *postXPost) Empty() bool { return value == nil }
func (value *postXPost) EditorSource() string {
	if value.Empty() {
		return ""
	}
	return componentJSON(value)
}
func (value *postXPost) SearchText() string { return "\n" + value.Description }
func (value *postXPost) Media() []componentMedia {
	if value.ImageID == "" {
		return nil
	}
	return []componentMedia{{ID: value.ImageID}}
}
func (value *postXPost) Node() ast.Node { return &xPostNode{Post: *value} }

type xPostNode struct {
	ast.BaseBlock
	Post postXPost
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
