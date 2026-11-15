package blog

// File components share upload, library, and field editing behavior.
type fileEditor struct {
	Kind, Field, Name, Source, Accept, Help, Limits string
	MaxFiles                                        int
}

func (p editorPage) DownloadsEditor() fileEditor {
	return fileEditor{
		Kind: "file-downloads", Field: "downloads", Name: "File downloads", Source: p.DownloadsJSON(),
		Accept: ".pdf,.zip,.txt", MaxFiles: maxPostDownloads,
		Help:   "Offer files below your post. Save with your post.",
		Limits: "Up to 20 files per post. PDF, ZIP, or UTF-8 text, up to 32 MiB per file. File storage: 1 GiB per site.",
	}
}

func (p editorPage) PDFEditor() fileEditor {
	return fileEditor{
		Kind: "pdf-preview", Field: "pdf", Name: "PDF preview", Source: p.PDFJSON(),
		Accept: ".pdf", MaxFiles: 1,
		Help:   "Embed a PDF below your post. Save with your post.",
		Limits: "One PDF, up to 32 MiB. Shares the site's 1 GiB file storage.",
	}
}
