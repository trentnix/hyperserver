package blog

import "github.com/yuin/goldmark/ast"

func audioDefinition() componentDefinition {
	return componentDefinition{
		Kind: "audio", Field: "audio", Payload: "audio",
		Name: "Audio", Summary: "An MP3 player with an optional transcript", EmptyMessage: "Choose an MP3 or remove the audio component.",
		TemplateFile: "templates/components/audio.html", Template: "blog.audioEditor", Icon: "blog.audioIcon", Script: "audio-editor.js",
		Parse:    func(source string) (componentContent, error) { return parseAudio(source) },
		Renderer: audioRenderer{},
	}
}

func (value *postAudio) Empty() bool { return value == nil }
func (value *postAudio) EditorSource() string {
	if value.Empty() {
		return ""
	}
	return componentJSON(value)
}
func (value *postAudio) SearchText() string {
	return "\n" + value.Title + "\n" + value.Description + "\n" + value.Transcript
}
func (value *postAudio) Media() []componentMedia {
	return []componentMedia{{ID: value.ID, Bytes: &value.Size}}
}
func (value *postAudio) Node() ast.Node { return &audioNode{Audio: *value} }
