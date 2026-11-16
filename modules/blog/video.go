package blog

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// Save the author's fields. Derive provider URLs locally instead of storing embed HTML.
type postVideo struct {
	URL     string `json:"url"`
	Title   string `json:"title,omitempty"`
	Caption string `json:"caption,omitempty"`
}

type videoSource struct {
	Provider, ID, Hash string
	Start              int
}

var youtubeVideoID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
var vimeoVideoID = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var vimeoVideoHash = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)

func videoURL(raw string) (videoSource, error) {
	unsupported := contentError("Enter a YouTube or Vimeo video URL.")
	u, err := linkURL(raw)
	if err != nil {
		return videoSource{}, err
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || u.RawPath != "" {
		return videoSource{}, unsupported
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	var source videoSource
	var start string
	switch strings.ToLower(u.Hostname()) {
	case "youtu.be", "www.youtu.be":
		if len(parts) != 1 {
			return source, unsupported
		}
		source.Provider, source.ID = "YouTube", parts[0]
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com", "youtube-nocookie.com", "www.youtube-nocookie.com":
		source.Provider = "YouTube"
		if len(parts) == 1 && parts[0] == "watch" && len(query["v"]) == 1 {
			source.ID = query.Get("v")
		} else if len(parts) == 2 && (parts[0] == "embed" || parts[0] == "shorts" || parts[0] == "live") {
			source.ID = parts[1]
		}
	case "vimeo.com", "www.vimeo.com":
		source.Provider = "Vimeo"
		if len(parts) == 1 || len(parts) == 2 {
			source.ID = parts[0]
			if len(parts) == 2 {
				source.Hash = parts[1]
			}
		}
	case "player.vimeo.com":
		source.Provider = "Vimeo"
		if len(parts) == 2 && parts[0] == "video" {
			source.ID = parts[1]
		}
	default:
		return source, unsupported
	}
	if source.Provider == "YouTube" {
		if !youtubeVideoID.MatchString(source.ID) {
			return source, unsupported
		}
		start = query.Get("t")
		if start == "" {
			start = query.Get("start")
		}
	} else {
		if !vimeoVideoID.MatchString(source.ID) || len(query["h"]) > 1 {
			return source, unsupported
		}
		if hash := query.Get("h"); hash != "" {
			if source.Hash != "" && source.Hash != hash {
				return source, unsupported
			}
			source.Hash = hash
		}
		if source.Hash != "" && !vimeoVideoHash.MatchString(source.Hash) {
			return source, unsupported
		}
		fragment, err := url.ParseQuery(u.Fragment)
		if err != nil {
			return source, unsupported
		}
		start = fragment.Get("t")
	}
	if start != "" {
		seconds, err := strconv.Atoi(start)
		if err != nil {
			duration, parseErr := time.ParseDuration(start)
			if parseErr != nil || duration%time.Second != 0 {
				return source, contentError("Use a video start time in whole seconds or a duration such as 1m30s.")
			}
			seconds = int(duration / time.Second)
		}
		if seconds < 0 || seconds > 7*24*60*60 {
			return source, contentError("Keep the video start time between zero and seven days.")
		}
		source.Start = seconds
	}
	return source, nil
}

func (v videoSource) viewingURL() string {
	if v.Provider == "YouTube" {
		u := "https://www.youtube.com/watch?v=" + v.ID
		if v.Start > 0 {
			u += "&t=" + strconv.Itoa(v.Start)
		}
		return u
	}
	u := "https://vimeo.com/" + v.ID
	if v.Hash != "" {
		u += "/" + v.Hash
	}
	if v.Start > 0 {
		u += "#t=" + strconv.Itoa(v.Start) + "s"
	}
	return u
}

func (v videoSource) embedURL() string {
	if v.Provider == "YouTube" {
		u := "https://www.youtube-nocookie.com/embed/" + v.ID + "?autoplay=0&playsinline=1"
		if v.Start > 0 {
			u += "&start=" + strconv.Itoa(v.Start)
		}
		return u
	}
	u := "https://player.vimeo.com/video/" + v.ID + "?"
	if v.Hash != "" {
		u += "h=" + v.Hash + "&"
	}
	u += "autoplay=0&dnt=1"
	if v.Start > 0 {
		u += "#t=" + strconv.Itoa(v.Start) + "s"
	}
	return u
}

func validateVideo(video *postVideo) error {
	if video == nil {
		return nil
	}
	if _, err := videoURL(video.URL); err != nil {
		return err
	}
	if !utf8.ValidString(video.Title) || utf8.RuneCountInString(video.Title) > 240 {
		return contentError("Keep the video title at most 240 characters.")
	}
	if !utf8.ValidString(video.Caption) || utf8.RuneCountInString(video.Caption) > 1000 {
		return contentError("Keep the video caption under 1,000 characters.")
	}
	return nil
}

func parseVideo(source string) (*postVideo, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	var video *postVideo
	if err := decodeComponentJSON(source, &video); err != nil || video == nil || !utf8.ValidString(source) {
		return nil, contentError("Unable to read the video. Check its source before saving.")
	}
	return video, validateVideo(video)
}

func (p postComponents) Video() *postVideo {
	for _, component := range p.Components {
		if component.Type == "video" {
			value, _ := component.Content.(*postVideo)
			return value
		}
	}
	return nil
}

func (p editorPage) VideoJSON() string { return p.componentSource("video") }

type videoNode struct {
	ast.BaseBlock
	Video postVideo
}

var videoKind = ast.NewNodeKind("Video")

func (*videoNode) Kind() ast.NodeKind              { return videoKind }
func (n *videoNode) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type videoRenderer struct{}

func (videoRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(videoKind, func(writer util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		video := node.(*videoNode).Video
		source, _ := videoURL(video.URL) // Component validation runs before creating this node.
		title := strings.TrimSpace(video.Title)
		writer.WriteString(`<section class="video-component" aria-label="Video"><h2>Video</h2>`)
		if title != "" {
			fmt.Fprintf(writer, `<h3 class="video-heading">%s</h3>`, html.EscapeString(title))
		} else {
			title = source.Provider + " video"
		}
		fmt.Fprintf(writer, `<div class="video-player"><iframe src="%s" title="%s" loading="lazy" referrerpolicy="strict-origin" allow="encrypted-media; fullscreen; picture-in-picture"></iframe></div>`, html.EscapeString(source.embedURL()), html.EscapeString(title))
		if video.Caption != "" {
			fmt.Fprintf(writer, `<p class="video-caption">%s</p>`, html.EscapeString(video.Caption))
		}
		writer.WriteString(`</section>`)
		return ast.WalkSkipChildren, nil
	})
}
