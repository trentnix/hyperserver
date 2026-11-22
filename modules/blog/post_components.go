package blog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

const unsupportedComponents = contentError("An unsupported component is preserved in the Markdown. Update or remove it before saving.")

// Components stay in each writing revision. Version 2 records their display
// order. Version 3 adds placement markers. Version 4 supports instance IDs.
type postComponents struct {
	Version    int             `json:"version"`
	Components []postComponent `json:"components"`
}

func (p postComponents) Order() []string {
	order := make([]string, 0, len(p.Components))
	for _, component := range p.Components {
		order = append(order, component.key())
	}
	return order
}

func decodeComponentJSON(source string, value any) error {
	decoder := json.NewDecoder(strings.NewReader(source))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("unexpected content after components")
	}
	return nil
}

func finalComponentsBlock(document ast.Node, source []byte) *ast.FencedCodeBlock {
	block, ok := document.LastChild().(*ast.FencedCodeBlock)
	if !ok || string(block.Language(source)) != "post-components" {
		return nil
	}
	return block
}

func parseComponents(source string) (postComponents, error) {
	return postComponentCatalog.parse(source)
}

func (catalog componentCatalog) parse(source string) (postComponents, error) {
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal([]byte(source), &header); err != nil || !utf8.ValidString(source) {
		return postComponents{}, unsupportedComponents
	}
	var raw []json.RawMessage
	switch header.Version {
	case 1:
		var legacy struct {
			Version   int            `json:"version"`
			Album     []albumPhoto   `json:"album,omitempty"`
			Downloads []postDownload `json:"downloads,omitempty"`
		}
		if err := decodeComponentJSON(source, &legacy); err != nil {
			return postComponents{}, unsupportedComponents
		}
		for _, entry := range []struct {
			kind, payload string
			value         any
			present       bool
		}{
			{"photo-album", "photos", legacy.Album, len(legacy.Album) > 0},
			{"file-downloads", "files", legacy.Downloads, len(legacy.Downloads) > 0},
		} {
			if entry.present {
				data, _ := json.Marshal(map[string]any{"type": entry.kind, entry.payload: entry.value})
				raw = append(raw, data)
			}
		}
	case 2, 3, 4:
		var envelope struct {
			Version    int               `json:"version"`
			Components []json.RawMessage `json:"components"`
		}
		if err := decodeComponentJSON(source, &envelope); err != nil {
			return postComponents{}, unsupportedComponents
		}
		raw = envelope.Components
	default:
		return postComponents{}, unsupportedComponents
	}
	result := postComponents{Version: max(2, header.Version)}
	seen := make(map[string]bool)
	for _, entry := range raw {
		var fields map[string]json.RawMessage
		if err := decodeComponentJSON(string(entry), &fields); err != nil {
			return result, unsupportedComponents
		}
		var kind string
		if err := json.Unmarshal(fields["type"], &kind); err != nil {
			return result, unsupportedComponents
		}
		definition, ok := catalog.find(kind)
		component := postComponent{Type: kind, payload: definition.Payload}
		if header.Version == 4 && fields["id"] != nil {
			if err := json.Unmarshal(fields["id"], &component.ID); err != nil || !strings.HasPrefix(component.ID, kind+":") {
				return result, unsupportedComponents
			}
			if _, ok := catalog.instance(component.ID); !ok {
				return result, unsupportedComponents
			}
			delete(fields, "id")
		}
		if !ok || seen[component.key()] || len(fields) != 2 || fields[definition.Payload] == nil {
			return result, unsupportedComponents
		}
		content, err := definition.decode(string(fields[definition.Payload]))
		if err != nil {
			var invalid contentError
			if !errors.As(err, &invalid) {
				return result, unsupportedComponents
			}
			return result, err
		}
		if content == nil || content.Empty() {
			return result, unsupportedComponents
		}
		seen[component.key()] = true
		component.Content = content
		result.Components = append(result.Components, component)
	}
	return result, nil
}

func parseComponentOrder(source string) ([]string, error) { return postComponentCatalog.order(source) }

func (catalog componentCatalog) order(source string) ([]string, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	var order []string
	if err := decodeComponentJSON(source, &order); err != nil {
		return nil, contentError("Unable to read the component order. Reload the editor and try again.")
	}
	seen := make(map[string]bool)
	for _, kind := range order {
		if _, ok := catalog.instance(kind); !ok || seen[kind] {
			return nil, contentError("Choose each component only once.")
		}
		seen[kind] = true
	}
	return order, nil
}

func splitPostComponents(source string) (string, postComponents, error) {
	return postComponentCatalog.split(source)
}

func (catalog componentCatalog) split(source string) (string, postComponents, error) {
	if !strings.Contains(source, "post-components") && !strings.Contains(source, "photo-album") {
		return source, postComponents{Version: 2}, nil
	}
	data := []byte(source)
	document := goldmark.New().Parser().Parse(text.NewReader(data))
	if block := finalComponentsBlock(document, data); block != nil {
		components, err := catalog.parse(string(block.Lines().Value(data)))
		if err != nil {
			return source, postComponents{}, err
		}
		start := strings.LastIndex(source[:block.Info.Segment.Start], "\n") + 1
		return strings.TrimRight(source[:start], "\r\n"), components, nil
	}
	body, album := splitPostAlbum(source)
	if _, supported := catalog.find("photo-album"); !supported && body != source {
		return source, postComponents{}, unsupportedComponents
	}
	photos, err := parseAlbum(album)
	if err != nil {
		return source, postComponents{}, err
	}
	result := postComponents{Version: 2}
	if len(photos) > 0 {
		result.Components = append(result.Components, postComponent{Type: "photo-album", payload: "photos", Content: albumContent(photos)})
	}
	return body, result, nil
}

func joinPostComponents(body string, input componentInput) (string, error) {
	return postComponentCatalog.join(body, input)
}

func (catalog componentCatalog) join(body string, input componentInput) (string, error) {
	// Unsupported stored content must remain visible and recoverable.
	if _, _, err := catalog.split(body); err != nil {
		return body, writingFieldError{err, "content"}
	}
	selected := make(map[string]postComponent)
	order, err := catalog.order(input["componentOrder"])
	if err != nil {
		return body, writingFieldError{err, "componentOrder"}
	}
	// An explicit order also records added components that have no content yet.
	for _, id := range order {
		component, err := catalog.requiredComponent(id, input)
		if err != nil {
			return body, err
		}
		selected[id] = component
	}
	for field, value := range input {
		if strings.Contains(field, ":") && strings.TrimSpace(value) != "" && selected[field].ID == "" {
			return body, writingFieldError{contentError("The component is missing from the order. Reload the editor and try again."), "componentOrder"}
		}
	}
	for _, definition := range catalog {
		if _, ok := selected[definition.Kind]; ok {
			continue
		}
		component, err := catalog.submittedComponent(definition.Kind, input)
		if err != nil {
			return body, err
		}
		if component.Content != nil && !component.Content.Empty() {
			selected[definition.Kind] = component
			order = append(order, definition.Kind)
		}
	}
	placed, err := validateComponentPlacements(body, selected)
	if err != nil {
		return body, err
	}
	if len(selected) == 0 {
		return body, nil
	}
	if !placed && len(selected) == 1 {
		if _, ok := selected["photo-album"]; ok {
			return joinPostAlbum(body, input["album"])
		}
	}
	components := postComponents{Version: 2}
	if placed {
		components.Version = 3
	}
	for _, id := range order {
		component := selected[id]
		if component.ID != "" {
			components.Version = 4
		}
		components.Components = append(components.Components, component)
	}
	data, err := json.Marshal(components)
	if err != nil {
		return body, err
	}
	combined := strings.TrimRight(body, "\r\n") + "\n\n```post-components\n" + string(data) + "\n```\n"
	_, decoded, err := catalog.split(combined)
	if err != nil || !slices.Equal(decoded.Order(), components.Order()) {
		return body, writingFieldError{contentError("Close any unfinished code or HTML blocks before adding components."), "content"}
	}
	return combined, nil
}

func (catalog componentCatalog) requiredComponent(id string, input componentInput) (postComponent, error) {
	component, err := catalog.submittedComponent(id, input)
	if err == nil && (component.Content == nil || component.Content.Empty()) {
		definition, _ := catalog.instance(id)
		err = writingFieldError{contentError(definition.EmptyMessage), catalog.field(component)}
	}
	return component, err
}

func (catalog componentCatalog) submittedComponent(id string, input componentInput) (postComponent, error) {
	definition, _ := catalog.instance(id)
	component := postComponent{Type: definition.Kind, payload: definition.Payload}
	field := definition.Field
	if id != definition.Kind {
		component.ID = id
		field = id
	}
	value, err := definition.Parse(input[field])
	if err != nil {
		fields := validationFields(err)
		if len(fields) == 0 {
			return component, writingFieldError{err, field}
		}
		if component.ID != "" {
			return component, writingFieldError{err, field}
		}
	}
	component.Content = value
	return component, err
}

// searchableWriting includes human component text without leaking the envelope's
// JSON keys, immutable IDs, or byte counts into body search results.
func searchableWriting(source string) string { return postComponentCatalog.searchText(source) }

func (catalog componentCatalog) searchText(source string) string {
	body, components, err := catalog.split(source)
	if err != nil || body == source {
		return source
	}
	var text strings.Builder
	if components.Version >= 3 {
		data := []byte(body)
		document := parsePlacementMarkdown(data)
		start := 0
		for node := document.FirstChild(); node != nil; node = node.NextSibling() {
			if componentMarker(node, data) != "" {
				line := node.Lines().At(0)
				text.WriteString(body[start:line.Start])
				start = line.Stop
			}
		}
		text.WriteString(body[start:])
	} else {
		text.WriteString(body)
	}
	for _, component := range components.Components {
		text.WriteString(component.Content.SearchText())
	}
	return text.String()
}

type componentsTransformer struct{ catalog componentCatalog }

func (transformer componentsTransformer) Transform(document *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	block := finalComponentsBlock(document, source)
	if block == nil {
		return
	}
	components, err := transformer.catalog.parse(string(block.Lines().Value(source)))
	if err != nil {
		return
	}
	pending := make(map[string]postComponent, len(components.Components))
	for _, component := range components.Components {
		pending[component.key()] = component
	}
	if components.Version >= 3 {
		for node := document.FirstChild(); node != nil && node != block; {
			next := node.NextSibling()
			id := componentMarker(node, source)
			if component, ok := pending[id]; ok {
				document.ReplaceChild(document, node, component.Content.Node())
				delete(pending, id)
			}
			node = next
		}
	}
	for _, component := range components.Components {
		if _, ok := pending[component.key()]; ok {
			document.InsertBefore(document, block, component.Content.Node())
		}
	}
	document.RemoveChild(document, block)
}
