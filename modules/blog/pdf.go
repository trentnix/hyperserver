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
		return contentError("Choose one uploaded PDF.")
	}
	return nil
}

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
		fmt.Fprintf(writer, `<section class="pdf-preview" aria-label="%s"><h2>PDF</h2><div class="pdf-preview-heading"><div><h3>%s</h3><span class="file-download-meta">PDF · %s</span></div><nav aria-label="PDF actions"><a href="/files/%s?preview=1" target="_blank" rel="noopener" aria-label="Open PDF in a new tab" hx-boost="false">Open PDF</a>`, pdfViewerName, label, file.SizeLabel(), file.ID)
		if !file.HideDownload {
			fmt.Fprintf(writer, `<a href="/files/%s" download hx-boost="false">Download</a>`, file.ID)
		}
		writer.WriteString(`</nav></div>`)
		if file.Description != "" {
			fmt.Fprintf(writer, `<p class="pdf-preview-description">%s</p>`, html.EscapeString(file.Description))
		}
		if file.Presentation {
			renderPDFPresentation(writer, file)
		} else {
			fmt.Fprintf(writer, `<iframe src="/files/%s?preview=1" title="%s" loading="lazy" class="pdf-preview-frame"></iframe>`, file.ID, label)
		}
		writer.WriteString(`</section>`)
		return ast.WalkSkipChildren, nil
	})
}

func renderPDFPresentation(writer util.BufWriter, file postDownload) {
	fmt.Fprintf(writer, `<div class="pdf-presentation" data-pdf-presentation data-pdf-url="/files/%s?preview=1" data-reader-only tabindex="0" role="region" aria-label="%s presentation">
<div class="pdf-slide" data-pdf-slide hidden><canvas aria-hidden="true"></canvas><div class="screen-reader-text" data-pdf-text></div></div>
<div class="pdf-presentation-controls" hidden>
<button type="button" data-pdf-previous aria-label="Previous page" disabled><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m15 5-7 7 7 7"/></svg></button>
<span data-pdf-page aria-live="polite">Loading PDF…</span>
<button type="button" data-pdf-next aria-label="Next page" disabled><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9 5 7 7-7 7"/></svg></button>
<button type="button" class="pdf-fullscreen" data-pdf-fullscreen aria-label="Enter fullscreen" hidden><svg viewBox="0 0 24 24" aria-hidden="true"><path class="pdf-fullscreen-enter" d="M8 3H3v5m13-5h5v5M3 16v5h5m13-5v5h-5"/><path class="pdf-fullscreen-exit" d="M3 8h5V3m8 0v5h5M8 21v-5H3m18 0h-5v5"/></svg></button>
<button type="button" data-pdf-retry hidden>Retry</button>
</div>
<p class="form-hint pdf-presentation-status" data-pdf-status role="status"></p>
<noscript><iframe src="/files/%s?preview=1" title="%s" loading="lazy" class="pdf-preview-frame"></iframe></noscript>
</div>`, file.ID, html.EscapeString(file.Label), file.ID, html.EscapeString(file.Label))
}
