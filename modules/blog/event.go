package blog

import (
	"fmt"
	"html"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

func eventDefinition() componentDefinition {
	return componentDefinition{
		Kind: "event", Field: "event", Payload: "event",
		Name: "Event", Summary: "A date, time, and location", EmptyMessage: "Enter an event name and start date or remove the event.",
		TemplateFile: "templates/components/event.html", Template: "blog.eventEditor", Icon: "blog.eventIcon", Script: "event-editor.js",
		Parse:        func(source string) (componentContent, error) { return parseEvent(source) },
		Decode:       func(source string) (componentContent, error) { return readEvent(source, true) },
		Renderer:     eventRenderer{},
		FeedRenderer: eventRenderer{feed: true},
	}
}

type postEvent struct {
	Title     string `json:"title"`
	StartDate string `json:"startDate"`
	AllDay    *bool  `json:"allDay,omitempty"`
	StartTime string `json:"startTime,omitempty"`
	EndDate   string `json:"endDate,omitempty"`
	EndTime   string `json:"endTime,omitempty"`
	TimeZone  string `json:"timeZone,omitempty"`
	Location  string `json:"location,omitempty"`
	MapURL    string `json:"mapURL,omitempty"`
	URL       string `json:"url,omitempty"`
	ImageID   string `json:"imageID,omitempty"`
	ImageAlt  string `json:"imageAlt,omitempty"`
	start     time.Time
	end       time.Time
	wallTime  bool
}

func (e *postEvent) Empty() bool { return e == nil }
func (e *postEvent) EditorSource() string {
	if e.Empty() {
		return ""
	}
	return componentJSON(e)
}
func (e *postEvent) SearchText() string {
	return "\n" + strings.Join([]string{e.Title, e.StartDate, e.EndDate, e.Location}, "\n")
}
func (e *postEvent) Media() []componentMedia {
	if e.ImageID == "" {
		return nil
	}
	return []componentMedia{{ID: e.ImageID}}
}
func (e *postEvent) Node() ast.Node { return &eventNode{Event: *e} }
func (e *postEvent) allDay() bool   { return e.StartTime == "" }

func parseEvent(source string) (*postEvent, error) {
	return readEvent(source, false)
}

func eventError(field, message string) error {
	return writingFieldError{contentError(message), "event." + field}
}

func readEvent(source string, saved bool) (*postEvent, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	var event *postEvent
	if err := decodeComponentJSON(source, &event); err != nil || event == nil || !utf8.ValidString(source) {
		return nil, contentError("Unable to read the event. Check its source before saving.")
	}
	event.Title = strings.TrimSpace(event.Title)
	event.Location = strings.TrimSpace(event.Location)
	event.MapURL = strings.TrimSpace(event.MapURL)
	event.URL = strings.TrimSpace(event.URL)
	event.TimeZone = strings.TrimSpace(event.TimeZone)
	if event.Title == "" || utf8.RuneCountInString(event.Title) > 240 {
		return nil, eventError("title", "Enter an event name of at most 240 characters.")
	}
	if utf8.RuneCountInString(event.Location) > 240 {
		return nil, eventError("location", "Keep the event location at most 240 characters.")
	}
	if event.MapURL != "" {
		if event.Location == "" {
			return nil, eventError("location", "Enter a location for the map link.")
		}
		if _, err := linkURL(event.MapURL); err != nil {
			return nil, eventError("mapURL", err.Error())
		}
	}
	if event.URL != "" {
		destination, err := linkURL(event.URL)
		if err != nil {
			return nil, eventError("url", err.Error())
		}
		if event.Location != "" && event.MapURL == "" && isEventMapLink(destination) {
			event.MapURL, event.URL = event.URL, ""
		}
	}
	if event.ImageID != "" && !validImageID(event.ImageID) {
		return nil, contentError("Choose an uploaded image for the event.")
	}
	event.ImageAlt = strings.TrimSpace(event.ImageAlt)
	if utf8.RuneCountInString(event.ImageAlt) > 500 {
		return nil, eventError("imageAlt", "Keep the image description at most 500 characters.")
	}
	if event.ImageID == "" {
		event.ImageAlt = ""
	}
	if err := event.readDates(saved); err != nil {
		return nil, err
	}
	return event, nil
}

func isEventMapLink(destination *url.URL) bool {
	switch strings.ToLower(destination.Hostname()) {
	case "maps.app.goo.gl", "maps.google.com", "maps.apple.com":
		return true
	case "google.com", "www.google.com", "goo.gl":
		return destination.Path == "/maps" || strings.HasPrefix(destination.Path, "/maps/")
	default:
		return false
	}
}

func (e *postEvent) readDates(saved bool) error {
	start, err := time.Parse(time.DateOnly, e.StartDate)
	if err != nil || start.Year() < 1 || start.Format(time.DateOnly) != e.StartDate {
		return eventError("startDate", "Choose an event start date.")
	}
	e.start = start
	if e.EndDate != "" {
		end, err := time.Parse(time.DateOnly, e.EndDate)
		if err != nil || end.Format(time.DateOnly) != e.EndDate || end.Before(start) {
			return eventError("endDate", "Choose an end date on or after the start date.")
		}
		e.end = end
	}
	if e.AllDay != nil && *e.AllDay != e.allDay() {
		if *e.AllDay {
			return eventError("startTime", "Clear the start time for an all-day event.")
		}
		return eventError("startTime", "Enter a start time or select All day.")
	}
	if e.allDay() {
		e.TimeZone = ""
		if e.EndTime != "" {
			return eventError("startTime", "Enter a start time before adding an end time.")
		}
		return nil
	}
	if e.EndTime == "" && e.EndDate != "" {
		return eventError("endTime", "Enter an end time when using an end date for a timed event.")
	}
	err = e.readTimes(e.TimeZone)
	if err == nil || !saved {
		return err
	}
	// A change in zone rules can invalidate a saved clock time. Preserve the
	// entered time without assigning it an offset under the new rules.
	e.wallTime = true
	return e.readTimes("UTC")
}

func (e *postEvent) readTimes(zoneName string) error {
	zone, err := loadTimeZone(zoneName)
	if err != nil {
		return eventError("timeZone", "Choose a time zone such as America/Chicago.")
	}
	start, err := eventTime(e.StartDate, e.StartTime, zone)
	if err != nil {
		return eventError("startTime", err.Error())
	}
	var end time.Time
	if e.EndTime != "" {
		endDate := e.EndDate
		if endDate == "" {
			endDate = e.StartDate
		}
		end, err = eventTime(endDate, e.EndTime, zone)
		if err != nil {
			return eventError("endTime", err.Error())
		}
		if !end.After(start) {
			return eventError("endTime", "Choose an end time after the start time.")
		}
	}
	e.start, e.end = start, end
	return nil
}

func eventTime(date, clock string, zone *time.Location) (time.Time, error) {
	at, err := parseLocalTime(date+" "+clock, zone)
	if err == errRepeatedTime {
		return time.Time{}, contentError("This time occurs twice when clocks change. Enter the event time in UTC to identify which one you mean.")
	}
	return at, err
}

type eventNode struct {
	ast.BaseBlock
	Event postEvent
}

var eventKind = ast.NewNodeKind("Event")

func (*eventNode) Kind() ast.NodeKind              { return eventKind }
func (n *eventNode) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type eventRenderer struct{ feed bool }

func (r eventRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(eventKind, func(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		event := node.(*eventNode).Event
		w.WriteString(`<section class="event-component" aria-label="Event">`)
		if event.ImageID != "" {
			path := "/images/" + event.ImageID
			label := "View event image"
			if event.ImageAlt != "" {
				label += ". " + event.ImageAlt
			}
			fmt.Fprintf(w, `<div class="event-image"><a href="%s" data-view-image hx-boost="false" aria-label="%s"><img src="%s" alt="%s" loading="lazy" decoding="async">`, path, html.EscapeString(label), path, html.EscapeString(event.ImageAlt))
			w.WriteString(`<span class="event-image-hint" aria-hidden="true"><svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3H3v5m13-5h5v5M3 16v5h5m13-5v5h-5"/></svg></span></a></div>`)
		}
		fmt.Fprintf(w, `<div class="event-content"><div class="event-date" aria-hidden="true" data-reader-only><span>%s</span><strong>%d</strong></div><div class="event-details"><h2>%s</h2><p class="event-when">`, event.start.Format("Jan"), event.start.Day(), html.EscapeString(event.Title))
		event.writeSchedule(w)
		w.WriteString("</p>")
		if event.Location != "" {
			fmt.Fprintf(w, `<p class="event-location">%s</p>`, html.EscapeString(event.Location))
		}
		var calendar string
		if !r.feed {
			calendar = event.calendar(time.Now())
		}
		if event.MapURL != "" || event.URL != "" || calendar != "" {
			w.WriteString(`<div class="event-links">`)
			if event.MapURL != "" {
				fmt.Fprintf(w, `<a class="event-link event-map-link" href="%s" target="_blank" rel="noopener" hx-boost="false" aria-label="View map for %s (opens in a new tab)">View map</a>`, html.EscapeString(event.MapURL), html.EscapeString(event.Location))
			}
			if event.URL != "" {
				fmt.Fprintf(w, `<a class="event-link" href="%s" hx-boost="false" aria-label="Event details for %s">Event details</a>`, html.EscapeString(event.URL), html.EscapeString(event.Title))
			}
			if calendar != "" {
				event.writeCalendarLink(w, calendar)
			}
			w.WriteString("</div>")
		}
		w.WriteString("</div></div></section>")
		return ast.WalkSkipChildren, nil
	})
}

func (e *postEvent) writeSchedule(w util.BufWriter) {
	const date = "Monday, Jan 2, 2006"
	hasEnd := !e.end.IsZero() && !e.end.Equal(e.start)
	if e.allDay() {
		fmt.Fprintf(w, `<time datetime="%s">%s</time>`, e.start.Format(time.DateOnly), e.start.Format(date))
		if hasEnd {
			fmt.Fprintf(w, ` – <time datetime="%s">%s</time>`, e.end.Format(time.DateOnly), e.end.Format(date))
		}
		w.WriteString("<br>All day")
		return
	}

	w.WriteString(e.start.Format(date))
	if hasEnd && e.start.Format(time.DateOnly) != e.end.Format(time.DateOnly) {
		w.WriteString(" – " + e.end.Format(date))
	}
	w.WriteString("<br>")
	_, startOffset := e.start.Zone()
	_, endOffset := e.end.Zone()
	e.writeTime(w, e.start, !hasEnd || startOffset != endOffset)
	if hasEnd {
		w.WriteString(" – ")
		e.writeTime(w, e.end, true)
	}
	if e.wallTime {
		fmt.Fprintf(w, " · %s", html.EscapeString(e.TimeZone))
	}
}

func (e *postEvent) writeTime(w util.BufWriter, at time.Time, showOffset bool) {
	display := "3:04 PM"
	machine := at.Format(time.RFC3339)
	_, offset := at.Zone()
	if offset%60 != 0 {
		// HTML time offsets allow hours and minutes only.
		machine = at.UTC().Format(time.RFC3339)
	}
	if e.wallTime {
		machine = at.Format("2006-01-02T15:04:05")
	} else if showOffset {
		display += " UTC"
		if offset%60 != 0 {
			display += "-07:00:00"
		} else if offset != 0 {
			display += "-07:00"
		}
	}
	fmt.Fprintf(w, `<time datetime="%s">%s</time>`, machine, at.Format(display))
}
