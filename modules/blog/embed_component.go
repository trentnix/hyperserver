package blog

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

const (
	embedFramePath      = "/embed/frame"
	maxEmbedURLBytes    = 2048
	maxEmbedDescription = 2000
)

// Match DNS hostnames before using an origin as a CSP source expression.
var embedHostname = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func embedURL(raw string) (*url.URL, error) {
	tooLong := contentError("Use an embed URL of at most 2,048 bytes after escaping.")
	if len(raw) > maxEmbedURLBytes {
		return nil, tooLong
	}
	u, err := linkURL(raw)
	if err != nil || u.Scheme != "https" {
		return nil, contentError("Enter an HTTPS embed URL on the standard port.")
	}
	host := strings.ToLower(u.Hostname())
	if len(host) > 253 || !embedHostname.MatchString(host) {
		return nil, contentError("Enter an HTTPS embed URL with a valid DNS hostname.")
	}
	u.Host = host
	if len(u.String()) > maxEmbedURLBytes {
		return nil, tooLong
	}
	return u, nil
}

type embedOrigins map[string]struct{}

func parseEmbedOrigins(values []string) (embedOrigins, error) {
	origins := make(embedOrigins, len(values))
	for _, value := range values {
		raw := strings.TrimSpace(value)
		u, err := embedURL(raw)
		if err != nil || (u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") {
			return nil, fmt.Errorf("blog.embedOrigins entry %q must be an HTTPS origin without a path, query, or fragment", value)
		}
		origins[u.Host] = struct{}{}
	}
	return origins, nil
}

type postEmbed struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

func embedDefinition() componentDefinition {
	return componentDefinition{
		Kind: "embed", Field: "embed", Payload: "embed",
		Name: "Embed", Summary: "An interactive page or widget", EmptyMessage: "Enter an embed URL and title or remove the embed.",
		TemplateFile: "templates/components/embed.html", Template: "blog.embedEditor", Icon: "blog.embedIcon", Script: "embed-editor.js",
		Parse:    func(source string) (componentContent, error) { return parseEmbed(source) },
		Renderer: embedRenderer{},
	}
}

func parseEmbed(source string) (*postEmbed, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	var embed *postEmbed
	if err := decodeComponentJSON(source, &embed); err != nil || embed == nil || !utf8.ValidString(source) {
		return nil, contentError("Unable to read the embed. Check its source before saving.")
	}
	if _, err := embed.validate(); err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(embed.Description) > maxEmbedDescription {
		return nil, writingFieldError{contentError("Use an embed description of at most 2,000 characters."), "embed.description"}
	}
	if strings.TrimSpace(embed.Description) == "" {
		embed.Description = ""
	}
	return embed, nil
}

func (e *postEmbed) validate() (*url.URL, error) {
	u, err := embedURL(strings.TrimSpace(e.URL))
	if err != nil {
		return nil, writingFieldError{err, "embed.url"}
	}
	e.Title = strings.TrimSpace(e.Title)
	if e.Title == "" || utf8.RuneCountInString(e.Title) > 240 {
		return nil, writingFieldError{contentError("Enter an embed title of at most 240 characters."), "embed.title"}
	}
	e.URL = u.String()
	return u, nil
}

func (e *postEmbed) Empty() bool             { return e == nil }
func (e *postEmbed) EditorSource() string    { return componentJSON(e) }
func (e *postEmbed) SearchText() string      { return "\n" + e.Title + "\n" + e.Description }
func (e *postEmbed) Media() []componentMedia { return nil }
func (e *postEmbed) Node() ast.Node          { return &embedNode{Embed: *e} }

type embedNode struct {
	ast.BaseBlock
	Embed postEmbed
}

var embedKind = ast.NewNodeKind("Embed")

func (*embedNode) Kind() ast.NodeKind              { return embedKind }
func (n *embedNode) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type embedRenderer struct{ origins embedOrigins }

func (reader embedRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(embedKind, func(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		e := node.(*embedNode).Embed
		u, _ := url.Parse(e.URL) // The component parser validated and normalized the URL.
		title := html.EscapeString(e.Title)
		w.WriteString(`<section class="web-embed" aria-label="Embed">`)
		if e.Description != "" {
			w.WriteString(`<div class="web-embed-description">`)
			// Plain Goldmark omits raw HTML and does not render nested components.
			if err := writeComponentMarkdown([]byte(e.Description), w); err != nil {
				return ast.WalkStop, err
			}
			w.WriteString(`</div>`)
		}
		if _, allowed := reader.origins[u.Host]; allowed {
			src := embedFramePath + "?" + url.Values{"title": {e.Title}, "url": {e.URL}}.Encode()
			fmt.Fprintf(w, `<div class="web-embed-frame" data-reader-only><iframe src="%s" title="%s" loading="lazy" referrerpolicy="no-referrer"></iframe></div>`, html.EscapeString(src), title)
		}
		fmt.Fprintf(w, `<p><a href="%s" target="_blank" rel="noopener noreferrer" hx-boost="false">Open %s</a></p></section>`, html.EscapeString(e.URL), title)
		return ast.WalkSkipChildren, nil
	})
}

// The frame document gives an approved provider its own CSP. The blog's
// document policy only needs to allow the same-origin frame.
func (m *module) embedFrame(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Add("Vary", "Accept")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'")
	check := r.Header.Get("Accept") == "application/json"
	query, err := url.ParseQuery(r.URL.RawQuery)
	e := postEmbed{URL: query.Get("url"), Title: query.Get("title")}
	var u *url.URL
	if err == nil && len(query) == 2 && len(query["url"]) == 1 && len(query["title"]) == 1 {
		u, err = e.validate()
	} else {
		err = contentError("Enter an HTTPS embed URL and title to preview it.")
	}
	if err != nil {
		if check {
			var field string
			if fields := validationFields(err); len(fields) == 2 {
				field = fields[1]
			}
			writeJSON(w, r, http.StatusBadRequest, struct {
				Field   string `json:"field,omitempty"`
				Message string `json:"message"`
			}{field, err.Error()})
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}
	_, allowed := m.embedOrigins[u.Host]
	if check {
		writeJSON(w, r, http.StatusOK, struct {
			URL     string `json:"url"`
			Allowed bool   `json:"allowed"`
		}{e.URL, allowed})
		return
	}
	if !allowed {
		http.Error(w, "This site has not been enabled for embeds. Add its origin to the blog settings to display it in a frame.", http.StatusForbidden)
		return
	}
	body, err := m.views["embedFrame"].RenderPartial(r, e)
	if err != nil {
		logger.LogRequestError(r, err)
		http.Error(w, "Unable to display the embed.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; frame-src https://"+u.Host+"; base-uri 'none'; form-action 'none'; frame-ancestors 'self'")
	w.Write([]byte(body))
}
