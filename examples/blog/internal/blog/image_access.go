package blog

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/trentnix/hyperserver/pkg/util"
	"golang.org/x/net/html"
)

// imageAccessRepository checks saved references against current publication state.
// References identify equivalent local URLs, including the configured public host.
type imageAccessRepository interface {
	ImageIsPublic(ctx context.Context, references ...string) (bool, error)
}

// publicImage allows only references from public content or the saved site profile.
// The request host never decides whether an absolute reference belongs to this site.
func (m *module) publicImage(r *http.Request, id string) (bool, error) {
	local := "/images/" + id
	references := []string{local}
	// Root-relative references do not require a public origin. If no trusted
	// absolute address can be built, absolute references cannot grant access.
	if public, err := util.BuildPublicURL(r, m.app.Config.HTTP, local, nil); err == nil {
		references = append(references, imageReference(public.String(), "/"))
		defaultPort := "443"
		if public.Scheme == "http" {
			defaultPort = "80"
		}
		if public.Port() == "" || public.Port() == defaultPort {
			// Protocol-relative references inherit the site's scheme. Keep their
			// explicit default port as an equivalent lookup key.
			public.Host = net.JoinHostPort(public.Hostname(), defaultPort)
			public.Scheme = ""
			references = append(references, imageReference(public.String(), "/"))
		}
	}
	profile := m.profile.Current()
	for _, raw := range []string{profile.ImageURL, profile.FaviconURL, profile.BackgroundURL} {
		if ref := imageReference(raw, "/"); ref != "" && slices.Contains(references, ref) {
			return true, nil
		}
	}
	return m.imageAccess.ImageIsPublic(r.Context(), references...)
}

// imageReference normalizes upload references for lookup. Queries and fragments
// do not change the file. A host is retained so foreign URLs cannot expose uploads.
func imageReference(raw, contentPath string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Opaque != "" || (u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	if u.Scheme != "" && u.Host == "" {
		return ""
	}
	u = (&url.URL{Path: contentPath}).ResolveReference(u)
	u.Path = path.Clean(u.Path)
	if !strings.HasPrefix(u.Path, "/images/") {
		return ""
	}
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		u.Host = u.Hostname()
		if strings.Contains(u.Host, ":") {
			u.Host = "[" + u.Host + "]"
		}
	}
	u.Scheme, u.RawPath, u.RawQuery, u.Fragment, u.RawFragment = "", "", "", "", ""
	u.ForceQuery = false
	u.Host = strings.ToLower(u.Host)
	return u.String()
}

// imageReferences indexes rendered images and download links, not URL-like text
// in code blocks, comments, or unused Markdown link definitions.
func imageReferences(source, contentPath, preview string) ([]string, error) {
	rendered, err := markdown(source)
	if err != nil {
		return nil, err
	}
	refs := make(map[string]bool)
	if preview != "" {
		refs["/images/"+preview] = true
	}
	tokens := html.NewTokenizer(strings.NewReader(string(rendered)))
	for {
		switch tokens.Next() {
		case html.ErrorToken:
			result := make([]string, 0, len(refs))
			for ref := range refs {
				result = append(result, ref)
			}
			return result, nil
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokens.Token()
			for _, attr := range token.Attr {
				if (token.Data == "img" && attr.Key == "src") || (token.Data == "a" && attr.Key == "href") {
					if ref := imageReference(attr.Val, contentPath); ref != "" {
						refs[ref] = true
					}
				}
			}
		}
	}
}
