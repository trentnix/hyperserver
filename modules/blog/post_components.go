package blog

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Components stay in each immutable writing revision. Version 2 records their
// display order. The reader also accepts the original version 1 envelope.
type postComponents struct {
	Version    int             `json:"version"`
	Components []postComponent `json:"components"`
}

type postComponent struct {
	Type   string         `json:"type"`
	Photos []albumPhoto   `json:"photos,omitempty"`
	Files  []postDownload `json:"files,omitempty"`
}

var componentTypes = [...]string{"photo-album", "file-downloads"}

func (p postComponents) Album() []albumPhoto {
	for _, component := range p.Components {
		if component.Type == "photo-album" {
			return component.Photos
		}
	}
	return nil
}

func (p postComponents) Downloads() []postDownload {
	for _, component := range p.Components {
		if component.Type == "file-downloads" {
			return component.Files
		}
	}
	return nil
}

func (p postComponents) Order() []string {
	order := make([]string, 0, len(p.Components))
	for _, component := range p.Components {
		order = append(order, component.Type)
	}
	return order
}

type postDownload struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Size        int64  `json:"size"`
}

func (d postDownload) TypeLabel() string {
	return strings.ToUpper(strings.TrimPrefix(filepath.Ext(d.ID), "."))
}
func (d postDownload) SizeLabel() string { return (mediaFile{Size: d.Size}).SizeLabel() }

func parseDownloads(source string) ([]postDownload, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	var entries []postDownload
	if err := decodeComponentJSON(source, &entries); err != nil || !utf8.ValidString(source) {
		return nil, contentError("Unable to read the file downloads. Check the component source before saving.")
	}
	if err := validateDownloads(entries); err != nil {
		return nil, err
	}
	return entries, nil
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

func validateDownloads(entries []postDownload) error {
	if len(entries) > maxPostDownloads {
		return contentError("Choose at most 20 file downloads per post.")
	}
	seen := make(map[string]bool)
	for _, entry := range entries {
		if !validDownloadID(entry.ID) || seen[entry.ID] || entry.Size < 0 || entry.Size > maxDownloadBytes {
			return contentError("Choose each uploaded file only once.")
		}
		if strings.TrimSpace(entry.Label) == "" || !utf8.ValidString(entry.Label) || utf8.RuneCountInString(entry.Label) > 240 {
			return contentError("Give each file a name of at most 240 characters.")
		}
		if !utf8.ValidString(entry.Description) || utf8.RuneCountInString(entry.Description) > 1000 {
			return contentError("Keep each file description under 1,000 characters.")
		}
		seen[entry.ID] = true
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
	unsupported := contentError("This post contains an unsupported component. Its source is preserved in Markdown. Update or remove it before saving.")
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal([]byte(source), &header); err != nil || !utf8.ValidString(source) {
		return postComponents{}, unsupported
	}
	var result postComponents
	switch header.Version {
	case 1:
		var legacy struct {
			Version   int            `json:"version"`
			Album     []albumPhoto   `json:"album,omitempty"`
			Downloads []postDownload `json:"downloads,omitempty"`
		}
		if err := decodeComponentJSON(source, &legacy); err != nil {
			return result, unsupported
		}
		result.Version = 2
		if len(legacy.Album) > 0 {
			result.Components = append(result.Components, postComponent{Type: "photo-album", Photos: legacy.Album})
		}
		if len(legacy.Downloads) > 0 {
			result.Components = append(result.Components, postComponent{Type: "file-downloads", Files: legacy.Downloads})
		}
	case 2:
		if err := decodeComponentJSON(source, &result); err != nil {
			return result, unsupported
		}
	default:
		return result, unsupported
	}
	seen := make(map[string]bool)
	for _, component := range result.Components {
		if seen[component.Type] {
			return result, unsupported
		}
		seen[component.Type] = true
		switch component.Type {
		case "photo-album":
			if len(component.Files) > 0 || len(component.Photos) == 0 {
				return result, unsupported
			}
			if err := validateComponentAlbum(component.Photos); err != nil {
				return result, err
			}
		case "file-downloads":
			if len(component.Photos) > 0 || len(component.Files) == 0 {
				return result, unsupported
			}
			if err := validateDownloads(component.Files); err != nil {
				return result, err
			}
		default:
			return result, unsupported
		}
	}
	return result, nil
}

func validateComponentAlbum(photos []albumPhoto) error {
	if len(photos) > maxAlbumPhotos {
		return contentError("Choose at most 50 photos for the album.")
	}
	seen := make(map[string]bool)
	for _, photo := range photos {
		if !validImageID(photo.ID) || seen[photo.ID] || strings.ContainsAny(photo.Caption, "\r\n") || !utf8.ValidString(photo.Caption) || utf8.RuneCountInString(photo.Caption) > 500 {
			return contentError("Choose uploaded photos with captions of at most 500 characters on one line.")
		}
		seen[photo.ID] = true
	}
	return nil
}

func parseComponentOrder(source string) ([]string, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	var order []string
	if err := decodeComponentJSON(source, &order); err != nil {
		return nil, contentError("Unable to read the component order. Reload the editor and try again.")
	}
	seen := make(map[string]bool)
	for _, kind := range order {
		if (kind != "photo-album" && kind != "file-downloads") || seen[kind] {
			return nil, contentError("Choose each component only once.")
		}
		seen[kind] = true
	}
	return order, nil
}

func albumSource(photos []albumPhoto) string {
	var lines []string
	for _, photo := range photos {
		caption := strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`).Replace(photo.Caption)
		lines = append(lines, "!["+caption+"](/images/"+photo.ID+")")
	}
	return strings.Join(lines, "\n")
}

func splitPostComponents(source string) (string, postComponents, error) {
	if !strings.Contains(source, "post-components") && !strings.Contains(source, "photo-album") {
		return source, postComponents{Version: 2}, nil
	}
	data := []byte(source)
	document := goldmark.New().Parser().Parse(text.NewReader(data))
	if block := finalComponentsBlock(document, data); block != nil {
		components, err := parseComponents(string(block.Lines().Value(data)))
		if err != nil {
			return source, postComponents{}, err
		}
		start := strings.LastIndex(source[:block.Info.Segment.Start], "\n") + 1
		return strings.TrimRight(source[:start], "\r\n"), components, nil
	}
	body, album := splitPostAlbum(source)
	photos, err := parseAlbum(album)
	if err != nil {
		return source, postComponents{}, err
	}
	result := postComponents{Version: 2}
	if len(photos) > 0 {
		result.Components = append(result.Components, postComponent{Type: "photo-album", Photos: photos})
	}
	return body, result, nil
}

func joinPostComponents(body, album, downloads, orderSource string) (string, error) {
	// An unsupported saved envelope must remain visible and recoverable. Never
	// silently strip fields from a newer component version.
	if _, _, err := splitPostComponents(body); err != nil {
		return body, writingFieldError{err, "content"}
	}
	photos, err := parseAlbum(album)
	if err != nil {
		return body, writingFieldError{err, "album"}
	}
	entries, err := parseDownloads(downloads)
	if err != nil {
		return body, writingFieldError{err, "downloads"}
	}
	order, err := parseComponentOrder(orderSource)
	if err != nil {
		return body, writingFieldError{err, "componentOrder"}
	}
	if len(entries) == 0 {
		return joinPostAlbum(body, album)
	}
	// Missing fields from older or ordinary forms retain the original default
	// order. An explicit order takes precedence for every component it includes.
	for _, kind := range componentTypes {
		found := false
		for _, current := range order {
			found = found || current == kind
		}
		if !found {
			order = append(order, kind)
		}
	}
	components := postComponents{Version: 2}
	for _, kind := range order {
		if kind == "photo-album" && len(photos) > 0 {
			components.Components = append(components.Components, postComponent{Type: kind, Photos: photos})
		}
		if kind == "file-downloads" && len(entries) > 0 {
			components.Components = append(components.Components, postComponent{Type: kind, Files: entries})
		}
	}
	data, err := json.Marshal(components)
	if err != nil {
		return body, err
	}
	combined := strings.TrimRight(body, "\r\n") + "\n\n```post-components\n" + string(data) + "\n```\n"
	_, decoded, err := splitPostComponents(combined)
	if err != nil || len(decoded.Downloads()) != len(entries) {
		return body, writingFieldError{contentError("Close any unfinished code or HTML blocks before adding components."), "content"}
	}
	return combined, nil
}

func (p editorPage) DownloadsJSON() string {
	if p.DownloadsInput != nil {
		return *p.DownloadsInput
	}
	_, components, _ := splitPostComponents(p.Markdown)
	if len(components.Downloads()) == 0 {
		return ""
	}
	data, _ := json.Marshal(components.Downloads())
	return string(data)
}

func (p editorPage) ComponentOrderJSON() string {
	if p.ComponentOrderInput != nil {
		return *p.ComponentOrderInput
	}
	_, components, _ := splitPostComponents(p.Markdown)
	data, _ := json.Marshal(components.Order())
	return string(data)
}

func (p editorPage) EditorComponents() []string {
	order, err := parseComponentOrder(p.ComponentOrderJSON())
	if err != nil {
		order = nil
	}
	for _, kind := range componentTypes {
		found := false
		for _, current := range order {
			found = found || current == kind
		}
		if !found {
			order = append(order, kind)
		}
	}
	return order
}

// searchableWriting includes human component text without leaking the envelope's
// JSON keys, immutable IDs, or byte counts into body search results.
func searchableWriting(source string) string {
	body, components, err := splitPostComponents(source)
	if err != nil || body == source {
		return source
	}
	var text strings.Builder
	text.WriteString(body)
	for _, component := range components.Components {
		for _, photo := range component.Photos {
			text.WriteString("\n" + photo.Caption)
		}
		for _, file := range component.Files {
			text.WriteString("\n" + file.Label + "\n" + file.Description)
		}
	}
	return text.String()
}

type downloadsNode struct {
	ast.BaseBlock
	Entries []postDownload
}

var downloadsKind = ast.NewNodeKind("FileDownloads")

func (*downloadsNode) Kind() ast.NodeKind              { return downloadsKind }
func (n *downloadsNode) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type componentsTransformer struct{}

func (componentsTransformer) Transform(document *ast.Document, reader text.Reader, _ parser.Context) {
	block := finalComponentsBlock(document, reader.Source())
	if block == nil {
		return
	}
	components, err := parseComponents(string(block.Lines().Value(reader.Source())))
	if err != nil {
		return
	}
	for _, component := range components.Components {
		switch component.Type {
		case "photo-album":
			document.InsertBefore(document, block, &albumNode{Photos: component.Photos})
		case "file-downloads":
			document.InsertBefore(document, block, &downloadsNode{Entries: component.Files})
		}
	}
	document.RemoveChild(document, block)
}

type downloadsRenderer struct{}

func (downloadsRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(downloadsKind, func(writer util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		writer.WriteString(`<section class="file-downloads" aria-label="File downloads"><h2>Downloads</h2><ul>`)
		for _, entry := range node.(*downloadsNode).Entries {
			fmt.Fprintf(writer, `<li><a href="/files/%s" hx-boost="false" download class="file-download-card" data-file-type="%s"><span class="file-download-type"><svg viewBox="0 0 32 40" aria-hidden="true"><path d="M7 2h12l8 8v28H7zM19 2v9h8"/></svg><span>%s</span></span><span class="file-download-details"><span class="file-download-name" title="%s">%s</span><span class="file-download-meta">%s</span>`, entry.ID, entry.TypeLabel(), entry.TypeLabel(), html.EscapeString(entry.Label), html.EscapeString(entry.Label), entry.SizeLabel())
			if entry.Description != "" {
				fmt.Fprintf(writer, `<span class="file-download-description">%s</span>`, html.EscapeString(entry.Description))
			}
			writer.WriteString(`</span><span class="file-download-action"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3v12m-4-4 4 4 4-4M5 17v4h14v-4"/></svg><span>Download</span></span></a></li>`)
		}
		writer.WriteString(`</ul></section>`)
		return ast.WalkSkipChildren, nil
	})
}
