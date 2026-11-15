package blog

import (
	"fmt"
	"html"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

func validatePDFPreview(files []postDownload) error {
	if len(files) > 1 || len(files) == 1 && downloadType(files[0].ID) != "application/pdf" {
		return contentError("Choose one uploaded PDF for the preview.")
	}
	return nil
}

func (p editorPage) PDFJSON() string { return p.fileSource("pdf-preview", p.PDFInput) }

type pdfNode struct {
	ast.BaseBlock
	File postDownload
}

var pdfKind = ast.NewNodeKind("PDFPreview")

func (*pdfNode) Kind() ast.NodeKind              { return pdfKind }
func (n *pdfNode) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type pdfRenderer struct{}

func (pdfRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(pdfKind, func(writer util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		file := node.(*pdfNode).File
		label := html.EscapeString(file.Label)
		fmt.Fprintf(writer, `<section class="pdf-preview" aria-label="PDF preview"><h2>PDF</h2><div class="pdf-preview-heading"><div><h3>%s</h3><span class="file-download-meta">PDF · %s</span></div><nav aria-label="PDF actions"><a href="/files/%s?preview=1" target="_blank" rel="noopener" aria-label="Open PDF in a new tab" hx-boost="false">Open PDF</a>`, label, file.SizeLabel(), file.ID)
		if !file.HideDownload {
			fmt.Fprintf(writer, `<a href="/files/%s" download hx-boost="false">Download</a>`, file.ID)
		}
		writer.WriteString(`</nav></div>`)
		if file.Description != "" {
			fmt.Fprintf(writer, `<p class="pdf-preview-description">%s</p>`, html.EscapeString(file.Description))
		}
		fmt.Fprintf(writer, `<iframe src="/files/%s?preview=1" title="%s" loading="lazy" class="pdf-preview-frame"></iframe></section>`, file.ID, label)
		return ast.WalkSkipChildren, nil
	})
}
