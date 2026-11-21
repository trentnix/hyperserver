package blog

import "github.com/yuin/goldmark/ast"

type pdfContent []postDownload

func pdfDefinition() componentDefinition {
	return componentDefinition{
		Kind: "pdf-preview", Field: "pdf", Payload: "files", Name: "PDF preview", Summary: "Show a PDF inline",
		EmptyMessage: "Choose a PDF or remove the PDF preview.",
		TemplateFile: "templates/components/downloads.html", Template: "blog.fileEditor", Icon: "blog.pdfIcon", Script: "downloads-editor.js",
		Parse: func(source string) (componentContent, error) {
			files, err := parseDownloads(source)
			if err == nil {
				err = validatePDFPreview(files)
			}
			return pdfContent(files), err
		},
		Renderer: pdfRenderer{},
		EditorData: func(source string, _ bool) any {
			return fileEditor{Source: source,
				Kind: "pdf-preview", Field: "pdf", Name: "PDF preview", Accept: ".pdf", MaxFiles: 1,
				Help:   "Readers can view the PDF without leaving the page.",
				Limits: "One PDF, up to 32 MiB. Shares the site's 1 GiB file storage.",
			}
		},
	}
}

func (files pdfContent) Empty() bool             { return len(files) == 0 }
func (files pdfContent) EditorSource() string    { return downloadsContent(files).EditorSource() }
func (files pdfContent) SearchText() string      { return fileSearchText(files) }
func (files pdfContent) Media() []componentMedia { return fileComponentMedia(files) }
func (files pdfContent) Node() ast.Node          { return &pdfNode{File: files[0]} }

func (p postComponents) PDF() []postDownload {
	for _, component := range p.Components {
		if value, ok := component.Content.(pdfContent); ok {
			return []postDownload(value)
		}
	}
	return nil
}
