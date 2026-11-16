package blog

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
)

// The blog owns its component catalog. Definitions are immutable application
// metadata, not a process-wide plugin registry or a framework extension point.
type componentDefinition struct {
	Kind, Field, Payload                 string
	Name, Summary, EmptyMessage          string
	TemplateFile, Template, Icon, Script string
	Parse                                func(string) (componentContent, error)
	Decode                               func(string) (componentContent, error)
	Renderer                             renderer.NodeRenderer
	EditorData                           func(source string, noLibrary bool) any
}

type componentContent interface {
	Empty() bool
	EditorSource() string
	SearchText() string
	Media() []componentMedia
	Node() ast.Node
}

type componentMedia struct {
	ID string
	// Bytes is present when saving must verify the recorded file size.
	Bytes *int64
}

type componentCatalog []componentDefinition

var postComponentCatalog = componentCatalog{
	albumDefinition(), downloadsDefinition(), pdfDefinition(), linkDefinition(), videoDefinition(), audioDefinition(), xPostDefinition(),
}

func (catalog componentCatalog) find(kind string) (componentDefinition, bool) {
	for _, definition := range catalog {
		if definition.Kind == kind {
			return definition, true
		}
	}
	return componentDefinition{}, false
}

func componentJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

// A decoded component keeps its typed content. Only its definition knows the
// envelope key, so another component never expands a shared tagged union.
type postComponent struct {
	Type    string
	payload string
	Content componentContent
}

func (component postComponent) MarshalJSON() ([]byte, error) {
	if component.payload == "" || component.Content == nil || component.Content.Empty() {
		return nil, fmt.Errorf("cannot encode an empty component")
	}
	return json.Marshal(map[string]any{"type": component.Type, component.payload: component.Content})
}

func (definition componentDefinition) decode(source string) (componentContent, error) {
	if definition.Decode != nil {
		return definition.Decode(source)
	}
	return definition.Parse(source)
}

// Submitted component fields are discovered from the same catalog as rendering.
type componentInput map[string]string

func componentForm(values url.Values) componentInput { return postComponentCatalog.form(values) }

func (catalog componentCatalog) form(values url.Values) componentInput {
	input := make(componentInput)
	for _, definition := range catalog {
		if values.Has(definition.Field) {
			input[definition.Field] = values.Get(definition.Field)
		}
	}
	if values.Has("componentOrder") {
		input["componentOrder"] = values.Get("componentOrder")
	}
	return input
}

func (p editorPage) componentSource(field string) string {
	if source, ok := p.ComponentInputs[field]; ok {
		return source
	}
	_, components, _ := splitPostComponents(p.Markdown)
	for _, component := range components.Components {
		definition, _ := postComponentCatalog.find(component.Type)
		if definition.Field == field {
			return component.Content.EditorSource()
		}
	}
	return ""
}

func (p editorPage) componentFields() map[string]string {
	result := make(map[string]string, len(postComponentCatalog)+1)
	_, components, _ := splitPostComponents(p.Markdown)
	for _, definition := range postComponentCatalog {
		result[definition.Field] = ""
	}
	for _, component := range components.Components {
		definition, _ := postComponentCatalog.find(component.Type)
		result[definition.Field] = component.Content.EditorSource()
	}
	for field, source := range p.ComponentInputs {
		result[field] = source
	}
	result["componentOrder"] = p.ComponentOrderJSON()
	return result
}

type componentEditor struct {
	componentDefinition
	Data any
}

type componentEditorData struct {
	Source    string
	NoLibrary bool
}

func (p editorPage) ComponentEditors() []componentEditor {
	result := make([]componentEditor, 0, len(postComponentCatalog))
	sources := p.componentFields()
	for _, kind := range p.EditorComponents() {
		definition, _ := postComponentCatalog.find(kind)
		source := sources[definition.Field]
		var data any = componentEditorData{Source: source, NoLibrary: p.NoLibrary}
		if definition.EditorData != nil {
			data = definition.EditorData(source, p.NoLibrary)
		}
		result = append(result, componentEditor{componentDefinition: definition, Data: data})
	}
	return result
}
