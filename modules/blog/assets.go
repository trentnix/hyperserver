package blog

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// validateAssetSeparation prevents the public asset route from exposing uploads.
// Resolve symlinks before comparing directories, including their ancestors.
func validateAssetSeparation(uploads, assets *os.Root) error {
	if assets == nil {
		return nil
	}
	paths := make([]string, 0, 2)
	for _, root := range []*os.Root{uploads, assets} {
		resolved, err := filepath.EvalSymlinks(root.Name())
		if err != nil {
			return err
		}
		absolute, err := filepath.Abs(resolved)
		if err != nil {
			return err
		}
		paths = append(paths, absolute)
	}
	for i := range 2 {
		relative, err := filepath.Rel(paths[i], paths[1-i])
		if err != nil {
			return err
		}
		if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("blog.assetsDirectory and blog.uploadsDirectory must not overlap")
		}
	}
	return nil
}

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
