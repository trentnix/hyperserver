package blog

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

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
	AliasFor                             string // Original stored kinds share the canonical component choice.
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
	albumDefinition(),
	downloadsDefinition(),
	pdfDefinition(),
	linkDefinition(),
	videoDefinition(),
	embedDefinition(),
	audioDefinition(),
	eventDefinition(),
	socialPostDefinition(),
	legacySocialPostDefinition(xProvider()),
	legacySocialPostDefinition(blueskyProvider()),
	legacySocialPostDefinition(threadsProvider()),
	legacySocialPostDefinition(facebookProvider()),
	comparisonDefinition(),
	relatedDefinition(),
}

func (catalog componentCatalog) find(kind string) (componentDefinition, bool) {
	for _, definition := range catalog {
		if definition.Kind == kind {
			return definition, true
		}
	}
	return componentDefinition{}, false
}

func (catalog componentCatalog) instance(id string) (componentDefinition, bool) {
	kind, suffix, extra := strings.Cut(id, ":")
	if extra {
		if len(suffix) != 32 || suffix != strings.ToLower(suffix) {
			return componentDefinition{}, false
		}
		if _, err := hex.DecodeString(suffix); err != nil {
			return componentDefinition{}, false
		}
	}
	return catalog.find(kind)
}

func componentJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

// A decoded component keeps its typed content. Only its definition knows the
// envelope key, so another component never expands a shared tagged union.
type postComponent struct {
	ID      string
	Type    string
	payload string
	Content componentContent
}

func (component postComponent) key() string {
	if component.ID != "" {
		return component.ID
	}
	return component.Type
}

func (catalog componentCatalog) field(component postComponent) string {
	if component.ID != "" {
		return component.ID
	}
	definition, _ := catalog.find(component.Type)
	return definition.Field
}

func (component postComponent) MarshalJSON() ([]byte, error) {
	if component.payload == "" || component.Content == nil || component.Content.Empty() {
		return nil, fmt.Errorf("cannot encode an empty component")
	}
	fields := map[string]any{"type": component.Type, component.payload: component.Content}
	if component.ID != "" {
		fields["id"] = component.ID
	}
	return json.Marshal(fields)
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
	for field := range values {
		if strings.Contains(field, ":") {
			if _, ok := catalog.instance(field); ok {
				input[field] = values.Get(field)
			}
		}
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
		result[postComponentCatalog.field(component)] = component.Content.EditorSource()
	}
	for field, source := range p.ComponentInputs {
		result[field] = source
	}
	result["componentOrder"] = p.ComponentOrderJSON()
	return result
}

func (p editorPage) ExtraComponents() map[string]string {
	fields := p.componentFields()
	extra := make(map[string]string)
	for field, value := range fields {
		if strings.Contains(field, ":") {
			extra[field] = value
		}
	}
	return extra
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
