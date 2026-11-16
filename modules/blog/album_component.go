package blog

import (
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
)

type albumContent []albumPhoto

func albumDefinition() componentDefinition {
	return componentDefinition{
		Kind: "photo-album", Field: "album", Payload: "photos", Name: "Photo album", Summary: "A grid of photos with captions",
		EmptyMessage: "Add at least one photo or remove the photo album.",
		TemplateFile: "templates/components/album.html", Template: "blog.albumEditor", Icon: "blog.albumIcon", Script: "album-editor.js",
		Parse: func(source string) (componentContent, error) {
			photos, err := parseAlbum(source)
			return albumContent(photos), err
		},
		Decode: func(source string) (componentContent, error) {
			var photos albumContent
			if err := decodeComponentJSON(source, &photos); err != nil {
				return nil, err
			}
			return photos, validateComponentAlbum(photos)
		},
		Renderer: albumRenderer{},
	}
}

func (photos albumContent) Empty() bool { return len(photos) == 0 }
func (photos albumContent) EditorSource() string {
	if photos.Empty() {
		return ""
	}
	return albumSource(photos) + "\n"
}
func (photos albumContent) SearchText() string {
	var text strings.Builder
	for _, photo := range photos {
		text.WriteString("\n" + photo.Caption)
	}
	return text.String()
}
func (photos albumContent) Media() []componentMedia {
	result := make([]componentMedia, 0, len(photos))
	for _, photo := range photos {
		result = append(result, componentMedia{ID: photo.ID})
	}
	return result
}
func (photos albumContent) Node() ast.Node { return &albumNode{Photos: photos} }

func (p postComponents) Album() []albumPhoto {
	for _, component := range p.Components {
		if photos, ok := component.Content.(albumContent); ok {
			return []albumPhoto(photos)
		}
	}
	return nil
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

func albumSource(photos []albumPhoto) string {
	var lines []string
	for _, photo := range photos {
		caption := strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`).Replace(photo.Caption)
		lines = append(lines, "!["+caption+"](/images/"+photo.ID+")")
	}
	return strings.Join(lines, "\n")
}
