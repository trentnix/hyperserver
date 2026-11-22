package blog

import (
	"fmt"
	"html"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

type postLink struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	SiteName    string `json:"siteName,omitempty"`
	ImageID     string `json:"imageID,omitempty"`
	HideImage   bool   `json:"hideImage,omitempty"`
}

func webLinkURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || !utf8.ValidString(raw) || u == nil || u.Opaque != "" || u.User != nil || u.Hostname() == "" ||
		(u.Scheme != "http" && u.Scheme != "https") || strings.ContainsAny(u.Host, "\\%") {
		return nil, contentError("Enter a complete http:// or https:// URL without a username or password.")
	}
	if strings.HasSuffix(u.Host, ":") || strings.ContainsAny(u.Hostname(), " \t\r\n") {
		return nil, contentError("Enter a valid destination hostname.")
	}
	return u, nil
}

// linkURL accepts standard web ports for destinations that may be fetched or embedded.
func linkURL(raw string) (*url.URL, error) {
	u, err := webLinkURL(raw)
	if err != nil {
		return nil, err
	}
	if port := u.Port(); port != "" && !(u.Scheme == "http" && port == "80" || u.Scheme == "https" && port == "443") {
		return nil, contentError("Use a URL with the standard HTTP or HTTPS port.")
	}
	return u, nil
}

func validateLink(link *postLink) error {
	if link == nil {
		return nil
	}
	if _, err := linkURL(link.URL); err != nil {
		return err
	}
	if strings.TrimSpace(link.Title) == "" || !utf8.ValidString(link.Title) || utf8.RuneCountInString(link.Title) > 240 {
		return contentError("Give the link a title of at most 240 characters.")
	}
	if !utf8.ValidString(link.Description) || utf8.RuneCountInString(link.Description) > 1000 || !utf8.ValidString(link.SiteName) || utf8.RuneCountInString(link.SiteName) > 200 {
		return contentError("Keep the description under 1,000 characters and the site name under 200 characters.")
	}
	if link.ImageID != "" && !validImageID(link.ImageID) {
		return contentError("Choose an uploaded image for the link preview.")
	}
	return nil
}

func parseLink(source string) (*postLink, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	var link *postLink
	if err := decodeComponentJSON(source, &link); err != nil || link == nil || !utf8.ValidString(source) {
		return nil, contentError("Unable to read the link preview. Check its source before saving.")
	}
	return link, validateLink(link)
}

func (p postComponents) Link() *postLink {
	for _, component := range p.Components {
		if component.Type == "link-preview" {
			value, _ := component.Content.(*postLink)
			return value
		}
	}
	return nil
}

type linkNode struct {
	ast.BaseBlock
	Link postLink
}

var linkKind = ast.NewNodeKind("LinkPreview")

func (*linkNode) Kind() ast.NodeKind              { return linkKind }
func (n *linkNode) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type linkRenderer struct{}

func (linkRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(linkKind, func(writer util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		link := node.(*linkNode).Link
		u, _ := linkURL(link.URL) // Component validation runs before creating this node.
		domain := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
		fmt.Fprintf(writer, `<section class="link-preview" aria-label="Link preview"><a class="link-preview-card" href="%s" hx-boost="false" rel="noopener"><span class="link-preview-details"><span class="link-preview-domain">%s</span><strong class="link-preview-title">%s</strong>`, html.EscapeString(link.URL), html.EscapeString(domain), html.EscapeString(link.Title))
		if link.Description != "" {
			fmt.Fprintf(writer, `<span class="link-preview-description">%s</span>`, html.EscapeString(link.Description))
		}
		if link.SiteName != "" {
			fmt.Fprintf(writer, `<span class="link-preview-site">%s</span>`, html.EscapeString(link.SiteName))
		}
		writer.WriteString(`</span>`)
		if link.ImageID != "" && !link.HideImage {
			fmt.Fprintf(writer, `<img src="/images/%s?variant=thumbnail" alt="" loading="lazy">`, link.ImageID)
		}
		writer.WriteString(`</a></section>`)
		return ast.WalkSkipChildren, nil
	})
}
