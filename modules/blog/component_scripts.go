package blog

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Ordinary module imports finish before editor initialization. Generate the entry
// point from the catalog once when routes bind, without a second registration list.
func (catalog componentCatalog) editorScript() http.Handler {
	var script strings.Builder
	seen := make(map[string]bool)
	for _, definition := range catalog {
		if !seen[definition.Script] {
			script.WriteString("import " + strconv.Quote("./"+definition.Script) + ";\n")
			seen[definition.Script] = true
		}
	}
	data := []byte(script.String())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, "component-editors.js", time.Time{}, bytes.NewReader(data))
	})
}
