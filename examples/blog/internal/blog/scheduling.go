package blog

import (
	"net/http"
	"strings"
	"time"
)

// currentTime supplies the publication clock. Tests can advance it without sleeping.
func (m *module) currentTime() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// postEditorData describes publication choices without changing saved content.
func (m *module) postEditorData(p Post) editorPage {
	now := m.currentTime()
	data := editorPage{Post: p, CanSchedule: !p.WasPublished(now)}
	if !p.Draft {
		data.Publication = "on"
	}
	if p.WasPublished(now) {
		data.PublishedSlug = p.Slug
	}
	if p.Scheduled(now) {
		data.Publication = "scheduled"
		data.ScheduleDate = p.Published.UTC().Format("2006-01-02")
		data.ScheduleTime = p.Published.UTC().Format("3:04 PM")
		data.ScheduledSlug, data.ScheduledAt = p.Slug, p.Published.Unix()
	}
	return data
}

// readPublication interprets the editor's explicit UTC schedule. Storage separately
// prevents rescheduling content whose first publication time has already passed.
func (m *module) readPublication(r *http.Request, p *Post) string {
	switch r.PostForm.Get("published") {
	case "":
		p.Draft = true
	case "on":
		p.Draft = false
	case "scheduled":
		value := strings.TrimSpace(r.PostForm.Get("publishDate")) + " " + strings.ToUpper(strings.TrimSpace(r.PostForm.Get("publishTime")))
		var date time.Time
		var err error
		for _, layout := range []string{"2006-01-02 15:04", "2006-01-02 3:04 PM", "2006-01-02 3:04PM"} {
			date, err = time.Parse(layout, value)
			if err == nil {
				break
			}
		}
		if err != nil || date.Year() < 1 || !date.After(m.currentTime()) {
			return "Choose a future publication date and time in UTC. Enter the time as 9:30 AM or 14:30."
		}
		p.Draft, p.Published = false, date
	default:
		return "Choose whether to publish the post."
	}
	return ""
}
