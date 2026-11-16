package blog

import "github.com/yuin/goldmark/ast"

func linkDefinition() componentDefinition {
	return componentDefinition{
		Kind: "link-preview", Field: "link", Payload: "link",
		Name: "Link preview", Summary: "A card with a title, description, and image", EmptyMessage: "Enter a link or remove the link preview.",
		TemplateFile: "templates/components/link.html", Template: "blog.linkEditor", Icon: "blog.linkIcon", Script: "link-editor.js",
		Parse:    func(source string) (componentContent, error) { return parseLink(source) },
		Renderer: linkRenderer{},
	}
}

func (value *postLink) Empty() bool { return value == nil }
func (value *postLink) EditorSource() string {
	if value.Empty() {
		return ""
	}
	return componentJSON(value)
}
func (value *postLink) SearchText() string {
	return "\n" + value.Title + "\n" + value.Description + "\n" + value.SiteName
}
func (value *postLink) Media() []componentMedia {
	if value.ImageID == "" {
		return nil
	}
	return []componentMedia{{ID: value.ImageID}}
}
func (value *postLink) Node() ast.Node { return &linkNode{Link: *value} }
