package blog

import (
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/util"
	"golang.org/x/net/html"
)

// sharingPreview supplies public Open Graph metadata for a complete HTML page.
// URL starts as a local content path and optional query, then becomes absolute.
type sharingPreview struct {
	Title, Description, URL, Image, Type string
}

// setSharing supplies the public description and Open Graph preview. Preview URLs
// use configuration, never request headers. URL errors do not suppress descriptions.
func (m *module) setSharing(c *content.Content, r *http.Request, preview sharingPreview, rendered template.HTML) {
	if c.IsHtmx() {
		return
	}
	data := c.LayoutData.(navigation)
	if preview.Title == "" {
		preview.Title = data.Profile.Name
	}
	if strings.TrimSpace(preview.Description) == "" {
		preview.Description = data.Profile.Bio
	}
	data.Description = preview.Description
	c.LayoutData = data

	local, err := url.Parse(preview.URL)
	if err != nil {
		logger.LogRequestError(r, err)
		return
	}
	base, err := util.BuildPublicURL(r, m.app.Config.HTTP, local.Path, nil)
	if err != nil {
		logger.LogRequestError(r, err)
		return
	}
	base.RawQuery = local.RawQuery
	preview.URL = base.String()
	preview.Image = sharingImageURL(preview.Image, base)
	if preview.Image == "" {
		preview.Image = firstSharingImage(rendered, base)
	}
	if preview.Image == "" {
		preview.Image = sharingImageURL(data.Profile.ImageURL, base)
	}

	data.Sharing = &preview
	c.LayoutData = data
}

// setPageSharing excludes pages hidden by an unpublished ancestor, even when an
// administrator can view them. Public requests have already passed that check.
func (m *module) setPageSharing(c *content.Content, r *http.Request, p Page, rendered template.HTML, admin bool) {
	if c.IsHtmx() || !p.Published {
		return
	}
	if admin {
		if _, err := m.pageStore.GetPageByID(r.Context(), p.ID, false); err != nil {
			if !errors.Is(err, errNotFound) {
				logger.LogRequestError(r, err)
			}
			return
		}
	}

	image := ""
	if p.PreviewImage != "" {
		image = p.PreviewURL()
	}
	m.setSharing(c, r, sharingPreview{Title: p.Title, Description: p.Summary, URL: p.URL(), Image: image, Type: "website"}, rendered)
}

// firstSharingImage reuses rendered Markdown instead of parsing the source again.
// Unsupported image URLs are skipped so a later suitable image can be selected.
func firstSharingImage(rendered template.HTML, base *url.URL) string {
	tokens := html.NewTokenizer(strings.NewReader(string(rendered)))
	for {
		switch tokens.Next() {
		case html.ErrorToken:
			return ""
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokens.Token()
			if token.Data != "img" {
				continue
			}
			for _, attr := range token.Attr {
				if attr.Key == "src" {
					if image := sharingImageURL(attr.Val, base); image != "" {
						return image
					}
				}
			}
		}
	}
}

// sharingImageURL resolves public HTTP(S) images without fetching remote content.
// SVGs are omitted because common sharing services do not support them.
func sharingImageURL(raw string, base *url.URL) string {
	ref, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || ref.Path == "" || strings.EqualFold(path.Ext(ref.Path), ".svg") {
		return ""
	}
	image := base.ResolveReference(ref)
	if image.User != nil || image.Hostname() == "" || (image.Scheme != "http" && image.Scheme != "https") {
		return ""
	}
	image.Fragment = ""
	return image.String()
}
