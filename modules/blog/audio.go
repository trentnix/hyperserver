package blog

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

type postAudio struct {
	ID           string `json:"id"`
	Size         int64  `json:"size"`
	Title        string `json:"title,omitempty"`
	Description  string `json:"description,omitempty"`
	Transcript   string `json:"transcript,omitempty"`
	HideDownload bool   `json:"hideDownload,omitempty"`
}

func parseAudio(source string) (*postAudio, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	var audio postAudio
	if err := decodeComponentJSON(source, &audio); err != nil || !utf8.ValidString(source) {
		return nil, contentError("Unable to read the audio. Check the component source before saving.")
	}
	return &audio, validateAudio(&audio)
}

func validateAudio(audio *postAudio) error {
	if !validDownloadID(audio.ID) || downloadType(audio.ID) != "audio/mpeg" || audio.Size <= 0 || audio.Size > maxDownloadBytes {
		return contentError("Choose one uploaded MP3 of at most 32 MiB.")
	}
	for _, field := range []struct {
		value string
		limit int
		error string
	}{
		{audio.Title, 240, "Keep the audio title under 240 characters."},
		{audio.Description, 1000, "Keep the audio description under 1,000 characters."},
		{audio.Transcript, 20000, "Keep the audio transcript under 20,000 characters."},
	} {
		if !utf8.ValidString(field.value) || utf8.RuneCountInString(field.value) > field.limit {
			return contentError(field.error)
		}
	}
	return nil
}

func (p postComponents) Audio() *postAudio {
	for _, component := range p.Components {
		if component.Type == "audio" {
			return component.Audio
		}
	}
	return nil
}

func (p editorPage) AudioJSON() string {
	if p.AudioInput != nil {
		return *p.AudioInput
	}
	_, components, _ := splitPostComponents(p.Markdown)
	if audio := components.Audio(); audio != nil {
		data, _ := json.Marshal(audio)
		return string(data)
	}
	return ""
}

type audioNode struct {
	ast.BaseBlock
	Audio postAudio
}

var audioKind = ast.NewNodeKind("Audio")

func (*audioNode) Kind() ast.NodeKind              { return audioKind }
func (n *audioNode) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type audioRenderer struct{}

func (audioRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(audioKind, func(writer util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		audio := node.(*audioNode).Audio
		label := strings.TrimSpace(audio.Title)
		if label == "" {
			label = "Audio recording"
		}
		writer.WriteString(`<section class="audio-component" aria-label="Audio"><h2>Audio</h2>`)
		if strings.TrimSpace(audio.Title) != "" {
			fmt.Fprintf(writer, `<h3 class="audio-title">%s</h3>`, html.EscapeString(audio.Title))
		}
		if strings.TrimSpace(audio.Description) != "" {
			fmt.Fprintf(writer, `<p class="audio-description">%s</p>`, html.EscapeString(audio.Description))
		}
		// Keep a fallback link inside the player for feed readers and browsers
		// without audio support, even when the separate download action is hidden.
		fmt.Fprintf(writer, `<audio controls preload="none" src="/files/%s?preview=1" aria-label="%s"><a href="/files/%s?preview=1" hx-boost="false">Listen to %s</a></audio>`, audio.ID, html.EscapeString(label), audio.ID, html.EscapeString(label))
		if !audio.HideDownload {
			fmt.Fprintf(writer, `<a class="audio-download" href="/files/%s" hx-boost="false" download>Download MP3 <span>(%s)</span></a>`, audio.ID, (mediaFile{Size: audio.Size}).SizeLabel())
		}
		if strings.TrimSpace(audio.Transcript) != "" {
			writer.WriteString(`<details class="audio-transcript"><summary>Transcript</summary><div>`)
			// Use plain Goldmark, without component transformers. A transcript
			// cannot recursively embed components or introduce raw HTML.
			if err := goldmark.New().Convert([]byte(audio.Transcript), writer); err != nil {
				return ast.WalkStop, err
			}
			writer.WriteString(`</div></details>`)
		}
		writer.WriteString(`</section>`)
		return ast.WalkSkipChildren, nil
	})
}
