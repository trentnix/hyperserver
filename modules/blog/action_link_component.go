package blog

import (
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

type postActionLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

func actionLinkDefinition() componentDefinition {
	return componentDefinition{
		Kind: "action-link", Field: "actionLink", Payload: "link",
		Name: "Action link", Summary: "A prominent link with a label you write", EmptyMessage: "Enter a label and URL or remove the action link.",
		TemplateFile: "templates/components/action-link.html", Template: "blog.actionLinkEditor", Icon: "blog.actionLinkIcon", Script: "action-link-editor.js",
		Parse:        func(source string) (componentContent, error) { return parseActionLink(source) },
		Renderer:     actionLinkRenderer{},
		FeedRenderer: actionLinkRenderer{plain: true},
	}
}

func parseActionLink(source string) (*postActionLink, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	var link *postActionLink
	if err := decodeComponentJSON(source, &link); err != nil || link == nil || !utf8.ValidString(source) {
		return nil, contentError("Unable to read the action link. Check its source before saving.")
	}
	link.Label = strings.TrimSpace(link.Label)
	if link.Label == "" || utf8.RuneCountInString(link.Label) > 240 {
		return nil, contentError("Enter a button label of at most 240 characters.")
	}
	destination, err := actionLinkURL(strings.TrimSpace(link.URL))
	if err != nil {
		return nil, err
	}
	link.URL = destination
	return link, nil
}

func actionLinkURL(raw string) (string, error) {
	var destination *url.URL
	var err error
	if strings.HasPrefix(raw, "/") {
		destination, err = url.Parse(raw)
		if err != nil || strings.HasPrefix(raw, "//") || strings.HasPrefix(destination.Path, "//") || strings.ContainsRune(destination.Path, '\\') {
			return "", contentError("Enter a site path beginning with a single slash.")
		}
	} else {
		destination, err = webLinkURL(raw)
		if err != nil {
			return "", err
		}
		if port := destination.Port(); port != "" {
			if _, err := strconv.ParseUint(port, 10, 16); err != nil {
				return "", contentError("Enter a URL with a valid port number.")
			}
		}
	}
	normalized := destination.String()
	if len(normalized) > 2048 {
		return "", contentError("Use a destination URL of at most 2,048 bytes.")
	}
	return normalized, nil
}

func (l *postActionLink) Empty() bool             { return l == nil }
func (l *postActionLink) EditorSource() string    { return componentJSON(l) }
func (l *postActionLink) SearchText() string      { return "\n" + l.Label }
func (l *postActionLink) Media() []componentMedia { return nil }
func (l *postActionLink) Node() ast.Node          { return &actionLinkNode{Link: *l} }

type actionLinkNode struct {
	ast.BaseBlock
	Link postActionLink
}

var actionLinkKind = ast.NewNodeKind("ActionLink")

func (*actionLinkNode) Kind() ast.NodeKind              { return actionLinkKind }
func (n *actionLinkNode) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type actionLinkRenderer struct{ plain bool }

func (r actionLinkRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(actionLinkKind, func(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		link := node.(*actionLinkNode).Link
		href, label := html.EscapeString(link.URL), html.EscapeString(link.Label)
		if r.plain {
			fmt.Fprintf(w, `<p><a href="%s">%s</a></p>`, href, label)
			return ast.WalkSkipChildren, nil
		}
		external := !strings.HasPrefix(link.URL, "/")
		fmt.Fprintf(w, `<p class="post-action-link"><a class="button-primary" href="%s"`, href)
		if external {
			fmt.Fprintf(w, ` target="_blank" rel="noopener noreferrer" hx-boost="false" aria-label="%s (opens in a new tab)"`, label)
		}
		fmt.Fprintf(w, `><span>%s</span>`, label)
		if external {
			w.WriteString(`<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M6 18 18 6M8 6h10v10"/></svg>`)
		}
		w.WriteString(`</a></p>`)
		return ast.WalkSkipChildren, nil
	})
}
