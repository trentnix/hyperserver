package blog

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"html"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuin/goldmark/util"
)

func (e *postEvent) writeCalendarLink(w util.BufWriter, calendar string) {
	filename := slugFromTitle(e.Title)
	if filename == "" {
		filename = "event-" + e.StartDate
	}
	fmt.Fprintf(w, `<a class="event-link event-calendar-link" href="data:text/calendar;charset=utf-8;base64,%s" download="%s.ics" hx-boost="false" aria-label="Add to calendar — %s">Add to calendar</a>`,
		base64.StdEncoding.EncodeToString([]byte(calendar)), filename, html.EscapeString(e.Title))
}

func (e *postEvent) calendar(issued time.Time) string {
	const date, dateTime = "20060102", "20060102T150405Z"
	// If a zone rule changed, the saved clock time no longer identifies an instant.
	if e.wallTime {
		return ""
	}
	var fields strings.Builder
	if e.allDay() {
		last := e.end
		if last.IsZero() {
			last = e.start
		}
		writeCalendarLine(&fields, "DTSTART;VALUE=DATE:"+e.start.Format(date))
		// The editor's end date is inclusive. iCalendar's end date is exclusive.
		end := last.AddDate(0, 0, 1)
		if end.Year() <= 9999 {
			writeCalendarLine(&fields, "DTEND;VALUE=DATE:"+end.Format(date))
		} else {
			fmt.Fprintf(&fields, "DURATION:P%dD\r\n", (end.Unix()-e.start.Unix())/86400)
		}
	} else {
		start, end := e.start.UTC(), e.end.UTC()
		if start.Year() < 1 || start.Year() > 9999 || end.Year() < 1 || end.Year() > 9999 {
			return ""
		}
		writeCalendarLine(&fields, "DTSTART:"+start.Format(dateTime))
		if !end.IsZero() {
			writeCalendarLine(&fields, "DTEND:"+end.Format(dateTime))
		}
	}
	writeCalendarLine(&fields, "SUMMARY:"+escapeCalendarText(e.Title))
	if e.Location != "" {
		writeCalendarLine(&fields, "LOCATION:"+escapeCalendarText(e.Location))
	}
	if destination, err := linkURL(e.URL); err == nil {
		writeCalendarLine(&fields, "URL:"+destination.String())
	}
	// Derive the identifier from the exported event so repeat downloads match.
	id := sha256.Sum256([]byte(fields.String()))
	return fmt.Sprintf("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//HyperServer//Blog Event//EN\r\nBEGIN:VEVENT\r\nUID:%x@hyperserver\r\nDTSTAMP:%s\r\n%sEND:VEVENT\r\nEND:VCALENDAR\r\n",
		id[:16], issued.UTC().Format(dateTime), fields.String())
}

func escapeCalendarText(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < ' ' && r != '\r' && r != '\n' && r != '\t' || r == 0x7f {
			return ' '
		}
		return r
	}, value)
	return strings.NewReplacer(`\`, `\\`, "\r\n", `\n`, "\r", `\n`, "\n", `\n`, ";", `\;`, ",", `\,`).Replace(value)
}

func writeCalendarLine(out *strings.Builder, line string) {
	for limit := 75; len(line) > limit; limit = 74 {
		end := limit
		for !utf8.RuneStart(line[end]) {
			end--
		}
		out.WriteString(line[:end])
		out.WriteString("\r\n ")
		line = line[end:]
	}
	out.WriteString(line)
	out.WriteString("\r\n")
}
