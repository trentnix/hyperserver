package blog

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

func TestComponentCatalogMetadata(t *testing.T) {
	kinds, fields := make(map[string]bool), make(map[string]bool)
	for _, definition := range postComponentCatalog {
		if definition.Kind == "" || kinds[definition.Kind] || definition.Field == "" || fields[definition.Field] || definition.Field == "componentOrder" {
			t.Fatal("component kinds and fields must be unique", definition)
		}
		kinds[definition.Kind], fields[definition.Field] = true, true
		if definition.Payload == "" || definition.Payload == "type" || definition.Template == "" || definition.Icon == "" || definition.Script == "" || definition.EmptyMessage == "" || definition.Parse == nil || definition.Renderer == nil {
			t.Fatal("incomplete component definition", definition)
		}
		for _, path := range []string{definition.TemplateFile, "static/js/" + definition.Script} {
			if _, err := fs.Stat(blogModuleFS, path); err != nil {
				t.Fatal(definition.Kind, err)
			}
		}
	}
}

// A local catalog adds a component without changing shared handlers, envelope
// fields, renderer registration, search, or the application's immutable catalog.
func TestComponentCatalogExtension(t *testing.T) {
	definition := componentDefinition{
		Kind: "test-note", Field: "note", Payload: "note", Name: "Note", Script: "test-note-editor.js", EmptyMessage: "Enter a note or remove it.",
		Parse: func(source string) (componentContent, error) {
			var note catalogNote
			if source == "" {
				return note, nil
			}
			if err := decodeComponentJSON(source, &note); err != nil {
				return nil, err
			}
			return note, nil
		},
		Renderer: catalogNoteRenderer{},
	}
	catalog := append(slices.Clone(postComponentCatalog), definition)
	input := catalog.form(url.Values{"note": {`{"text":"A <new> component"}`}, "componentOrder": {`["test-note"]`}})
	source, err := catalog.join("Body", input)
	if err != nil {
		t.Fatal(err)
	}
	body, components, err := catalog.split(source)
	if err != nil || body != "Body" || !slices.Equal(components.Order(), []string{"test-note"}) {
		t.Fatal(body, components, err)
	}
	var restored catalogNote
	if err := json.Unmarshal([]byte(components.Components[0].Content.EditorSource()), &restored); err != nil || restored.Text != "A <new> component" {
		t.Fatal("editor source changed", restored, err)
	}
	script := catalog.editorScript()
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		response := httptest.NewRecorder()
		script.ServeHTTP(response, httptest.NewRequest(method, "/assets/js/component-editors.js", nil))
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
			t.Fatal("editor module response", response.Code, response.Header())
		}
		if method == http.MethodGet {
			for _, name := range []string{"downloads-editor.js", "test-note-editor.js"} {
				if strings.Count(response.Body.String(), "import \"./"+name+"\";") != 1 {
					t.Fatal("editor import missing or duplicated", response.Body.String())
				}
			}
		} else if response.Body.Len() != 0 {
			t.Fatal("HEAD returned module bytes")
		}
	}

	if got := catalog.searchText(source); got != "Body\nA <new> component" {
		t.Fatal("search bypassed the definition", got)
	}
	rendered, err := markdownComponents(source, catalog)
	if err != nil || !strings.Contains(string(rendered), "<aside>A &lt;new&gt; component</aside>") {
		t.Fatal("reader bypassed the definition", rendered, err)
	}
	if _, _, err := splitPostComponents(source); err == nil {
		t.Fatal("a local catalog changed the application catalog")
	}
	if _, err := catalog.join("Body", componentInput{"componentOrder": `["test-note"]`}); !slices.Equal(validationFields(err), []string{"note"}) {
		t.Fatal("empty component lost its field", err)
	}
	for _, raw := range []string{
		`{"version":2,"components":[{"type":"test-note","note":{"text":"One"}},{"type":"test-note","note":{"text":"Two"}}]}`,
		`{"version":2,"components":[{"type":"test-note","note":{"text":"One"},"extra":true}]}`,
		`{"version":2,"components":[{"type":"test-note","note":{"text":"One","extra":true}}]}`,
	} {
		if _, err := catalog.parse(raw); err == nil {
			t.Fatal("accepted unsupported content", raw)
		}
	}
}

type catalogNote struct {
	Text string `json:"text"`
}

func (note catalogNote) Empty() bool             { return note.Text == "" }
func (note catalogNote) EditorSource() string    { data, _ := json.Marshal(note); return string(data) }
func (note catalogNote) SearchText() string      { return "\n" + note.Text }
func (note catalogNote) Media() []componentMedia { return nil }
func (note catalogNote) Node() ast.Node          { return &catalogNoteNode{Value: note.Text} }

var catalogNoteKind = ast.NewNodeKind("CatalogNote")

type catalogNoteNode struct {
	ast.BaseBlock
	Value string
}

func (*catalogNoteNode) Kind() ast.NodeKind { return catalogNoteKind }
func (node *catalogNoteNode) Dump(source []byte, level int) {
	ast.DumpHelper(node, source, level, nil, nil)
}

type catalogNoteRenderer struct{}

func (catalogNoteRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(catalogNoteKind, func(writer util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			_, err := fmt.Fprintf(writer, "<aside>%s</aside>", html.EscapeString(node.(*catalogNoteNode).Value))
			return ast.WalkSkipChildren, err
		}
		return ast.WalkContinue, nil
	})
}

func TestUnsupportedComponentPayloadIsContentError(t *testing.T) {
	for _, payload := range []string{`{"unexpected":"value"}`, `["wrong shape"]`, `[{"id":"unused","unknown":true}]`} {
		_, err := parseComponents(`{"version":2,"components":[{"type":"photo-album","photos":` + payload + `}]}`)
		var invalid contentError
		if !errors.As(err, &invalid) {
			t.Fatal("malformed stored payload must remain an author-correctable error", err)
		}
	}
}

func TestComponentCatalogWithoutAlbum(t *testing.T) {
	catalog := componentCatalog{audioDefinition()}
	for _, payload := range []string{"![A photo](/images/" + strings.Repeat("a", 64) + ".png)", "", "unsupported photo source"} {
		source := "Before\n\n```photo-album\n" + payload + "\n```\n"
		body, components, err := catalog.split(source)
		if err == nil || body != source || len(components.Components) != 0 {
			t.Fatalf("a catalog without albums must preserve unsupported legacy source: body=%q components=%v err=%v", body, components, err)
		}
		rendered, err := markdownComponents(source, catalog)
		if err != nil || !strings.Contains(string(rendered), `<pre><code class="language-photo-album">`) {
			t.Fatal("unsupported legacy albums must remain code", rendered, err)
		}
	}
	source := "Ordinary text about photo-album components."
	if body, _, err := catalog.split(source); body != source || err != nil {
		t.Fatal("ordinary text became unsupported content", body, err)
	}
}
