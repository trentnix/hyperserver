package content

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"path"

	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

// Templates contains immutable page and HTMX template sets. Share it between
// requests, but give each request its own Content and data. Use PrepareTemplates
// to construct it. The zero value cannot render.
type Templates struct {
	page     *parsedTemplates
	fragment *parsedTemplates
}

type parsedTemplates struct {
	template *template.Template
	root     string
}

// PrepareTemplates parses both response variants in this order: manager layouts,
// manager content, supplied content, then manager components. Configure cm before
// calling this during module initialization and return any error to startup.
// A nil cm uses only the supplied files. The returned sets contain no request data
// and do not reread files or track later manager changes. To reload, prepare new
// sets and replace them only after draining requests, such as on server restart.
func PrepareTemplates(cm *contentservice.ContentManagerService, contents ...TemplatePath) (*Templates, error) {
	c := &Content{ContentManager: cm, Contents: contents}
	page, err := parseTemplates(c.templatePaths())
	if err != nil {
		return nil, fmt.Errorf("prepare page templates: %w", err)
	}
	c.HTMX.Request.Enabled = true
	fragment, err := parseTemplates(c.templatePaths())
	if err != nil {
		return nil, fmt.Errorf("prepare fragment templates: %w", err)
	}
	return &Templates{page: page, fragment: fragment}, nil
}

func parseTemplates(files []TemplatePath) (*parsedTemplates, error) {
	if len(files) == 0 {
		return nil, NewErrNoTemplates(fmt.Errorf("No templates have been set"))
	}
	root := path.Base(files[0].Path)
	var tmpl *template.Template
	funcs := template.FuncMap{
		"renderPartial": func(name string, data any) (template.HTML, error) {
			if name == "" {
				return "", fmt.Errorf("partial template name was not specified")
			}
			if tmpl.Lookup(name) == nil {
				return "", fmt.Errorf("partial template not found: %s", name)
			}
			var out bytes.Buffer
			if err := tmpl.ExecuteTemplate(&out, name, data); err != nil {
				return "", err
			}
			// Only html/template output crosses this trust boundary. Its input was escaped.
			return template.HTML(out.String()), nil
		},
	}

	tmpl = template.New(root).Funcs(funcs)
	for _, file := range files {
		if file.FS == nil || !fs.ValidPath(file.Path) {
			return nil, NewErrParsingTemplates(fmt.Errorf("template requires a filesystem and a valid path: %q", file.Path))
		}
		data, err := fs.ReadFile(file.FS, file.Path)
		if err != nil {
			return nil, NewErrParsingTemplates(err)
		}
		target := tmpl
		if name := path.Base(file.Path); name != root {
			target = tmpl.New(name)
		}
		if _, err := target.Parse(string(data)); err != nil {
			return nil, NewErrParsingTemplates(err)
		}
	}
	return &parsedTemplates{template: tmpl, root: root}, nil
}
