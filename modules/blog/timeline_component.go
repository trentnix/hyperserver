package blog

import (
	"fmt"
	"html"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

const maxTimelineMilestones = 20
const maxMilestoneDetailRunes = 2000

type timelineMilestone struct {
	Date   string `json:"date"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`

	dateLabel string
}

type postTimeline struct {
	Title      string              `json:"title,omitempty"`
	Style      string              `json:"style,omitempty"`
	Milestones []timelineMilestone `json:"milestones"`
}

func timelineDefinition() componentDefinition {
	return componentDefinition{
		Kind: "timeline", Field: "timeline", Payload: "timeline",
		Name: "Timeline", Summary: "Dated milestones in chronological order", EmptyMessage: "Add a milestone or remove the timeline.",
		TemplateFile: "templates/components/timeline.html", Template: "blog.timelineEditor", Icon: "blog.timelineIcon", Script: "timeline-editor.js",
		Parse: func(source string) (componentContent, error) { return parseTimeline(source) }, Renderer: timelineRenderer{},
		EditorData: func(data componentEditorData) any {
			return struct {
				Source                   string
				MaxMilestones, MaxDetail int
			}{data.Source, maxTimelineMilestones, maxMilestoneDetailRunes}
		},
	}
}

func parseTimeline(source string) (*postTimeline, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	var timeline *postTimeline
	if err := decodeComponentJSON(source, &timeline); err != nil || timeline == nil || !utf8.ValidString(source) {
		return nil, contentError("Unable to read the timeline. Check its source before saving.")
	}
	timeline.Title = strings.TrimSpace(timeline.Title)
	if utf8.RuneCountInString(timeline.Title) > 240 {
		return nil, contentError("Keep the timeline heading within 240 characters.")
	}
	switch timeline.Style {
	case "", "white", "parchment", "forest", "brick":
	default:
		return nil, contentError("Choose an available timeline color.")
	}
	if len(timeline.Milestones) > maxTimelineMilestones {
		return nil, contentError(fmt.Sprintf("Add at most %d milestones.", maxTimelineMilestones))
	}
	for i := range timeline.Milestones {
		item := &timeline.Milestones[i]
		item.Date = strings.TrimSpace(item.Date)
		item.dateLabel = timelineDateLabel(item.Date)
		if item.dateLabel == "" {
			return nil, contentError(fmt.Sprintf("Enter a valid date for milestone %d. Use YYYY, YYYY-MM, or YYYY-MM-DD.", i+1))
		}
		item.Title = strings.TrimSpace(item.Title)
		if item.Title == "" || utf8.RuneCountInString(item.Title) > 240 {
			return nil, contentError(fmt.Sprintf("Enter a title of at most 240 characters for milestone %d.", i+1))
		}
		if utf8.RuneCountInString(item.Detail) > maxMilestoneDetailRunes {
			return nil, contentError(fmt.Sprintf("Keep the detail for milestone %d within 2,000 characters.", i+1))
		}
		if strings.TrimSpace(item.Detail) == "" {
			item.Detail = ""
		}
	}
	return timeline, nil
}

func timelineDateLabel(date string) string {
	var layout, display string
	switch len(date) {
	case len("2006"):
		layout, display = "2006", "2006"
	case len("2006-01"):
		layout, display = "2006-01", "Jan 2006"
	case len(time.DateOnly):
		layout, display = time.DateOnly, "Jan 2, 2006"
	default:
		return ""
	}
	parsed, err := time.Parse(layout, date)
	if err != nil || parsed.Year() < 1 || parsed.Format(layout) != date {
		return ""
	}
	return parsed.Format(display)
}

func (t *postTimeline) Empty() bool             { return t == nil || len(t.Milestones) == 0 }
func (t *postTimeline) EditorSource() string    { return componentJSON(t) }
func (t *postTimeline) Media() []componentMedia { return nil }

func (t *postTimeline) SearchText() string {
	var text strings.Builder
	text.WriteString("\n" + t.Title)
	for _, item := range t.Milestones {
		text.WriteString("\n" + item.Date + " " + item.Title + "\n" + item.Detail)
	}
	return text.String()
}

func (t *postTimeline) Node() ast.Node {
	items := slices.Clone(t.Milestones)
	// Canonical dates compare in calendar order. A shorter date comes first
	// within its period. Identical dates keep the author's order.
	slices.SortStableFunc(items, func(a, b timelineMilestone) int { return strings.Compare(a.Date, b.Date) })
	return &timelineNode{title: t.Title, style: t.Style, milestones: items}
}

type timelineNode struct {
	ast.BaseBlock
	title, style string
	milestones   []timelineMilestone
}

var timelineKind = ast.NewNodeKind("Timeline")

func (*timelineNode) Kind() ast.NodeKind              { return timelineKind }
func (n *timelineNode) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type timelineRenderer struct{}

func (timelineRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(timelineKind, func(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		timeline := node.(*timelineNode)
		label := "Timeline"
		if timeline.title != "" {
			label = html.EscapeString(timeline.title)
		}
		fmt.Fprintf(w, `<section class="post-timeline" aria-label="%s"`, label)
		if timeline.style != "" {
			fmt.Fprintf(w, ` data-timeline-style="%s"`, html.EscapeString(timeline.style))
		}
		w.WriteString(">")
		if timeline.title != "" {
			fmt.Fprintf(w, "<h3>%s</h3>", label)
		}
		w.WriteString(`<ol class="timeline-milestones" role="list">`)
		for _, item := range timeline.milestones {
			fmt.Fprintf(w, `<li><time datetime="%s">%s</time><p class="timeline-title"><strong>%s</strong></p>`,
				item.Date, item.dateLabel, html.EscapeString(item.Title))
			if item.Detail != "" {
				w.WriteString(`<div class="timeline-detail">`)
				if err := writeComponentMarkdown([]byte(item.Detail), w); err != nil {
					return ast.WalkStop, err
				}
				w.WriteString(`</div>`)
			}
			w.WriteString(`</li>`)
		}
		w.WriteString(`</ol></section>`)
		return ast.WalkSkipChildren, nil
	})
}
