package blog

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
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
	FeedRenderer                         renderer.NodeRenderer
	TransformDocument                    parser.ASTTransformer
	EditorData                           func(componentEditorData) any
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
	actionLinkDefinition(),
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
	detailsDefinition(),
	calloutDefinition(),
	tableOfContentsDefinition(),
	relatedDefinition(),
}

func (catalog componentCatalog) forFeed() componentCatalog {
	feed := slices.Clone(catalog)
	for i, definition := range feed {
		if definition.FeedRenderer != nil {
			feed[i].Renderer = definition.FeedRenderer
		}
	}
	return feed
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

type componentEditorForm struct {
	BodyMarkdown     string
	RequireBody      bool
	HasComponents    bool
	noLibrary        bool
	fileStorageLimit string
	fields           componentInput
}

func newComponentEditorForm(source string, input componentInput) componentEditorForm {
	body, components, _ := splitPostComponents(source)
	fields := make(componentInput, len(postComponentCatalog)+1)
	for _, definition := range postComponentCatalog {
		fields[definition.Field] = ""
	}
	for _, component := range components.Components {
		fields[postComponentCatalog.field(component)] = component.Content.EditorSource()
	}
	fields["componentOrder"] = componentJSON(components.Order())
	maps.Copy(fields, input)
	return componentEditorForm{BodyMarkdown: body, HasComponents: len(components.Components) > 0, fields: fields}
}

func (p componentEditorForm) baseline() componentInput {
	fields := maps.Clone(p.fields)
	fields["content"] = p.BodyMarkdown
	return fields
}

func (p componentEditorForm) ComponentOrderJSON() string { return p.fields["componentOrder"] }

func (p componentEditorForm) ExtraComponents() map[string]string {
	extra := make(map[string]string)
	for field, value := range p.fields {
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
	Source, FileStorageLimit string
	NoLibrary                bool
}

func (componentEditorForm) ComponentChoices() []componentDefinition {
	choices := make([]componentDefinition, 0, len(postComponentCatalog))
	for _, definition := range postComponentCatalog {
		if definition.AliasFor == "" {
			choices = append(choices, definition)
		}
	}
	slices.SortStableFunc(choices, func(a, b componentDefinition) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return choices
}

func (p componentEditorForm) EditorComponents() []string {
	order, err := parseComponentOrder(p.ComponentOrderJSON())
	if err != nil {
		order = nil
	}
	order = slices.DeleteFunc(order, func(id string) bool { return strings.Contains(id, ":") })
	for _, definition := range postComponentCatalog {
		if !slices.Contains(order, definition.Kind) {
			order = append(order, definition.Kind)
		}
	}
	return order
}

func (p componentEditorForm) ComponentEditors() []componentEditor {
	result := make([]componentEditor, 0, len(postComponentCatalog))
	for _, kind := range p.EditorComponents() {
		definition, _ := postComponentCatalog.find(kind)
		fields := componentEditorData{Source: p.fields[definition.Field], FileStorageLimit: p.fileStorageLimit, NoLibrary: p.noLibrary}
		var data any = fields
		if definition.EditorData != nil {
			data = definition.EditorData(fields)
		}
		result = append(result, componentEditor{componentDefinition: definition, Data: data})
	}
	return result
}
