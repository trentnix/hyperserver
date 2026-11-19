package blog

import (
	"fmt"
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
	zone := m.siteTimeZone()
	data := editorPage{Post: p, TimeZone: zone.String(), ScheduleZone: zone.String(), CanSchedule: p.Draft || !p.WasPublished(now)}
	data.SaveActions = editorActions("post", !p.Draft && !p.Scheduled(now), p.Scheduled(now))
	if !p.Draft {
		data.Publication = "on"
	}
	if p.WasPublished(now) {
		data.PublishedSlug = p.Slug
	}
	if p.Scheduled(now) {
		at := p.Published.In(zone)
		// UTC can represent an existing instant whose local clock time repeats.
		if repeatedLocalTime(at) {
			at = at.UTC()
			data.ScheduleZone = "UTC"
		}
		data.Publication = "scheduled"
		data.ScheduleDate = at.Format(time.DateOnly)
		data.ScheduleTime = at.Format("3:04 PM")
		data.ScheduledSlug, data.ScheduledAt = p.Slug, p.Published.Unix()
	}
	return data
}

// scheduleZone honors the zone shown when the schedule was entered, including
// when an author restores a browser copy after the site's default changed.
func (m *module) scheduleZone(name string) (*time.Location, error) {
	zone := m.siteTimeZone()
	if name == "" || name == zone.String() {
		return zone, nil
	}
	return loadTimeZone(name)
}

// readPublication interprets the schedule as a local clock time. Storage separately
// prevents scheduling currently published content until it is saved as a draft.
func (m *module) readPublication(r *http.Request, p *Post) string {
	switch r.PostForm.Get("published") {
	case "":
		p.Draft = true
	case "on":
		p.Draft = false
	case "scheduled":
		zone, err := m.scheduleZone(r.PostForm.Get("publishTimeZone"))
		if err != nil {
			return "The publication time zone is invalid. Reopen the editor and check the schedule."
		}
		value := strings.TrimSpace(r.PostForm.Get("publishDate")) + " " + strings.ToUpper(strings.TrimSpace(r.PostForm.Get("publishTime")))
		var date time.Time
		for _, layout := range []string{localTimeLayout, "2006-01-02 3:04 PM", "2006-01-02 3:04PM"} {
			date, err = time.Parse(layout, value)
			if err == nil {
				break
			}
		}
		if err == nil && date.Year() > 0 {
			at, err := parseLocalTime(date.Format(localTimeLayout), zone)
			if err != nil {
				return err.Error() + " Choose another publication time."
			}
			if at.After(m.currentTime()) {
				p.Draft, p.Published = false, at.UTC()
				return ""
			}
		}
		return fmt.Sprintf("Choose a future publication date and time in %s. Enter the time as 9:30 AM or 14:30.", zone)
	default:
		return "Choose whether to publish the post."
	}
	return ""
}
