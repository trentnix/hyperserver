package blog

import "github.com/yuin/goldmark/ast"

func videoDefinition() componentDefinition {
	return componentDefinition{
		Kind: "video", Field: "video", Payload: "video",
		Name: "Video", Summary: "A YouTube or Vimeo player", EmptyMessage: "Enter a video URL or remove the video component.",
		TemplateFile: "templates/components/video.html", Template: "blog.videoEditor", Icon: "blog.videoIcon", Script: "video-editor.js",
		Parse:    func(source string) (componentContent, error) { return parseVideo(source) },
		Renderer: videoRenderer{},
	}
}

func (value *postVideo) Empty() bool { return value == nil }
func (value *postVideo) EditorSource() string {
	if value.Empty() {
		return ""
	}
	return componentJSON(value)
}
func (value *postVideo) SearchText() string      { return "\n" + value.Title + "\n" + value.Caption }
func (value *postVideo) Media() []componentMedia { return nil }
func (value *postVideo) Node() ast.Node          { return &videoNode{Video: *value} }
