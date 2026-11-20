package blog

import (
	"fmt"
	"html"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

type downloadsContent []postDownload

func downloadsDefinition() componentDefinition {
	return componentDefinition{
		Kind: "file-downloads", Field: "downloads", Payload: "files", Name: "File downloads", Summary: "PDFs, ZIP archives, text files, and MP3s",
		EmptyMessage: "Add at least one file or remove the file downloads component.",
		TemplateFile: "templates/components/downloads.html", Template: "blog.fileEditor", Icon: "blog.downloadsIcon", Script: "downloads-editor.js",
		Parse: func(source string) (componentContent, error) {
			files, err := parseDownloads(source)
			return downloadsContent(files), err
		},
		Renderer: downloadsRenderer{},
		EditorData: func(source string, _ bool) any {
			return fileEditor{Source: source,
				Kind: "file-downloads", Field: "downloads", Name: "File downloads", Accept: ".pdf,.zip,.txt,.mp3", MaxFiles: maxComponentDownloads,
				Help:   "Attach files readers can download.",
				Limits: "Up to 20 files. PDF, ZIP, UTF-8 text, or MP3, up to 32 MiB per file. File storage: 1 GiB per site.",
			}
		},
	}
}

func (files downloadsContent) Empty() bool { return len(files) == 0 }
func (files downloadsContent) EditorSource() string {
	if files.Empty() {
		return ""
	}
	return componentJSON(files)
}
func (files downloadsContent) SearchText() string      { return fileSearchText(files) }
func (files downloadsContent) Media() []componentMedia { return fileComponentMedia(files) }
func (files downloadsContent) Node() ast.Node          { return &downloadsNode{Entries: files} }

func (p postComponents) Downloads() []postDownload {
	for _, component := range p.Components {
		if value, ok := component.Content.(downloadsContent); ok {
			return []postDownload(value)
		}
	}
	return nil
}

// File components share upload, library, and field editing behavior.
type fileEditor struct {
	Kind, Field, Name, Source, Accept, Help, Limits string
	MaxFiles                                        int
}

type postDownload struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Size        int64  `json:"size"`
	// PDF previews can hide their download link. Missing settings keep it visible.
	HideDownload bool `json:"hideDownload,omitempty"`
	// PDF previews can opt into a single-page presentation instead of the native viewer.
	Presentation bool `json:"presentation,omitempty"`
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

func validateDownloads(entries []postDownload) error {
	if len(entries) > maxComponentDownloads {
		return contentError("Choose at most 20 files for this component.")
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

type downloadsNode struct {
	ast.BaseBlock
	Entries []postDownload
}

var downloadsKind = ast.NewNodeKind("FileDownloads")

func (*downloadsNode) Kind() ast.NodeKind              { return downloadsKind }
func (n *downloadsNode) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

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

func fileComponentMedia(files []postDownload) []componentMedia {
	result := make([]componentMedia, 0, len(files))
	for _, file := range files {
		result = append(result, componentMedia{ID: file.ID, Bytes: &file.Size})
	}
	return result
}

func fileSearchText(files []postDownload) string {
	var text strings.Builder
	for _, file := range files {
		text.WriteString("\n" + file.Label + "\n" + file.Description)
	}
	return text.String()
}
