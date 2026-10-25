package blog

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// openLocalAssets opens optional public assets without embedding them in the binary.
// The caller closes the root after HTTP requests have drained.
func openLocalAssets(directory, workingDirectory string) (*os.Root, error) {
	if directory == "" {
		return nil, nil
	}
	if !filepath.IsAbs(directory) {
		directory = filepath.Join(workingDirectory, directory)
	}
	return os.OpenRoot(directory)
}

// localAsset serves top-level public files without directory listings or hidden files.
// os.Root prevents paths and symlinks from escaping the configured directory.
func (m *module) localAsset(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	name := r.PathValue("name")
	if !fs.ValidPath(name) || strings.HasPrefix(name, ".") || strings.ContainsAny(name, `/\`) {
		http.NotFound(w, r)
		return
	}
	file, err := m.localAssets.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, name, info.ModTime(), file)
}
