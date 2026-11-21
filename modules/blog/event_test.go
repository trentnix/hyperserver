package blog

import (
	"net/url"
	"strings"
	"testing"
)

func TestEventComponent(t *testing.T) {
	allDay := true
	imageID := strings.Repeat("a", 64) + ".png"
	for _, tc := range []struct {
		name  string
		event postEvent
		want  []string
	}{
		{
			name: "timed",
			event: postEvent{Title: "Talk <one>", StartDate: "2027-01-12", StartTime: "18:30", EndTime: "20:00",
				TimeZone: "America/Chicago", Location: "<Community hall>", MapURL: "https://maps.example/?pin=hall&zoom=16",
				URL: "https://example.com/event?x=1&y=2", ImageID: imageID, ImageAlt: "The hall <at night>"},
			want: []string{`<h2>Talk &lt;one&gt;</h2>`, "2027-01-12T18:30:00-06:00", "2027-01-12T20:00:00-06:00", "6:30 PM</time>", "8:00 PM UTC-06:00", "&lt;Community hall&gt;", "https://example.com/event?x=1&amp;y=2",
				`href="https://maps.example/?pin=hall&amp;zoom=16" target="_blank" rel="noopener" hx-boost="false" aria-label="View map for &lt;Community hall&gt; (opens in a new tab)">View map</a>`,
				`href="/images/` + imageID + `" data-view-image hx-boost="false" aria-label="View event image. The hall &lt;at night&gt;"`,
				`src="/images/` + imageID + `" alt="The hall &lt;at night&gt;"`},
		},
		{
			name: "offset changes across event",
			event: postEvent{Title: "Overnight event", StartDate: "2027-03-13", StartTime: "22:00",
				EndDate: "2027-03-14", EndTime: "04:00", TimeZone: "America/Chicago"},
			want: []string{"10:00 PM UTC-06:00", "4:00 AM UTC-05:00"},
		},
		{
			name:  "all day",
			event: postEvent{Title: "Open studio", StartDate: "2027-04-16", EndDate: "2027-04-18", AllDay: &allDay},
			want:  []string{`datetime="2027-04-16"`, `datetime="2027-04-18"`, "All day"},
		},
		{
			name:  "legacy all day",
			event: postEvent{Title: "Open studio", StartDate: "2027-04-16", Location: "Online"},
			want:  []string{`datetime="2027-04-16"`, "All day", `<p class="event-location">Online</p>`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, err := joinPostComponents("Body", componentInput{"event": componentJSON(tc.event), "componentOrder": `["event"]`})
			if err != nil {
				t.Fatal(err)
			}
			editor := newComponentEditorForm(source, nil)
			if got := editor.fields["event"]; got != componentJSON(tc.event) {
				t.Fatalf("event did not round trip: %s", got)
			}
			html, err := markdown(source)
			if err != nil {
				t.Fatal(err)
			}
			for _, fragment := range tc.want {
				if !strings.Contains(string(html), fragment) {
					t.Fatalf("missing %q in %s", fragment, html)
				}
			}
			feed, err := markdownAt(source, &url.URL{Scheme: "https", Host: "blog.example", Path: "/post/s/event"})
			if err != nil || !strings.Contains(string(feed), tc.event.StartDate) {
				t.Fatalf("expected event dates in feed: %s, %v", feed, err)
			}
			if tc.event.ImageID != "" {
				for _, attr := range []string{"href", "src"} {
					if !strings.Contains(string(feed), attr+`="https://blog.example/images/`+imageID+`"`) {
						t.Fatalf("expected an absolute event image %s in feed: %s", attr, feed)
					}
				}
			}
			if strings.Contains(string(feed), ">View map</a>") != (tc.event.MapURL != "") {
				t.Fatalf("unexpected map link in feed: %s", feed)
			}
			if text := searchableWriting(source); !strings.Contains(text, "\n"+tc.event.Title) || !strings.Contains(text, tc.event.StartDate) || strings.Contains(text, `"startDate"`) {
				t.Fatalf("unexpected event search text: %s", text)
			}
		})
	}
}

func TestSavedEventWithChangedTimeZoneRules(t *testing.T) {
	event := postEvent{Title: "Opening", StartDate: "2027-03-14", StartTime: "01:30", TimeZone: "America/Chicago"}
	source, err := joinPostComponents("Body", componentInput{"event": componentJSON(event), "video": `{"url":"https://youtu.be/M7lc1UVf-VE"}`})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a clock time that was valid when saved and is now skipped.
	source = strings.Replace(source, `"startTime":"01:30"`, `"startTime":"02:30"`, 1)
	rendered, err := markdown(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`class="event-component"`, `datetime="2027-03-14T02:30:00"`, "2:30 AM</time> · America/Chicago", `class="video-component"`} {
		if !strings.Contains(string(rendered), fragment) {
			t.Fatalf("missing %q in %s", fragment, rendered)
		}
	}
	event.StartTime = "02:30"
	if got := newComponentEditorForm(source, nil).fields["event"]; got != componentJSON(event) {
		t.Fatalf("the editor must preserve the saved wall time: %s", got)
	}
	if _, err := parseEvent(componentJSON(event)); err == nil || strings.Join(validationFields(err), " ") != "event event.startTime" {
		t.Fatalf("editing the skipped time must identify the start time: %v", err)
	}
}

func TestEventValidation(t *testing.T) {
	allDay, timed := true, false
	valid := postEvent{Title: "Talk", StartDate: "2027-01-12", StartTime: "09:30", TimeZone: "America/Chicago"}
	for _, tc := range []struct {
		name string
		edit func(*postEvent)
		want string
	}{
		{"missing name", func(e *postEvent) { e.Title = " " }, "event name"},
		{"invalid date", func(e *postEvent) { e.StartDate = "2027-02-29" }, "start date"},
		{"invalid zone", func(e *postEvent) { e.TimeZone = "Local" }, "time zone"},
		{"skipped time", func(e *postEvent) { e.StartDate, e.StartTime = "2027-03-14", "02:30" }, "skipped"},
		{"repeated time", func(e *postEvent) { e.StartDate, e.StartTime = "2027-11-07", "01:30" }, "occurs twice"},
		{"end before start", func(e *postEvent) { e.EndTime = "09:00" }, "after the start time"},
		{"end date without end time", func(e *postEvent) { e.EndDate = "2027-01-13" }, "Enter an end time"},
		{"end time without start time", func(e *postEvent) { e.StartTime, e.EndTime = "", "10:00" }, "Enter a start time"},
		{"timed event missing time", func(e *postEvent) { e.AllDay, e.StartTime = &timed, "" }, "Enter a start time or select All day"},
		{"all-day event with time", func(e *postEvent) { e.AllDay = &allDay }, "Clear the start time"},
		{"invalid image", func(e *postEvent) { e.ImageID = "../example.png" }, "Choose an uploaded image"},
		{"long image description", func(e *postEvent) { e.ImageAlt = strings.Repeat("a", 501) }, "at most 500 characters"},
		{"unsafe link", func(e *postEvent) { e.URL = "javascript:alert(1)" }, "https://"},
		{"map link without location", func(e *postEvent) { e.MapURL = "https://maps.example/" }, "Enter a location for the map link"},
		{"unsafe map link", func(e *postEvent) { e.Location, e.MapURL = "Community hall", "javascript:alert(1)" }, "https://"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := valid
			tc.edit(&value)
			if _, err := parseEvent(componentJSON(value)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
	for _, source := range []string{"", "null", `{"unexpected":true}`} {
		if _, err := joinPostComponents("Body", componentInput{"event": source, "componentOrder": `["event"]`}); err == nil {
			t.Fatalf("accepted invalid event %q", source)
		}
	}
}

func TestEventMapLinkFromWebsite(t *testing.T) {
	for _, tc := range []struct {
		name, website, location, mapLink string
		wantMap                          bool
	}{
		{name: "shared map", website: "https://maps.app.goo.gl/example", location: "Community hall", wantMap: true},
		{name: "direct map", website: "https://www.google.com/maps/place/Community+hall", location: "Community hall", wantMap: true},
		{name: "event website", website: "https://www.google.com/calendar", location: "Community hall"},
		{name: "explicit map", website: "https://maps.app.goo.gl/venue", location: "Community hall", mapLink: "https://maps.example/entrance"},
		{name: "no location", website: "https://maps.app.goo.gl/example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := postEvent{Title: "Open studio", StartDate: "2027-04-16", Location: tc.location, URL: tc.website, MapURL: tc.mapLink}
			source := "```post-components\n" + componentJSON(map[string]any{
				"version": 2, "components": []map[string]any{{"type": "event", "event": event}},
			}) + "\n```"
			if tc.wantMap {
				event.MapURL, event.URL = event.URL, ""
			}
			if got := newComponentEditorForm(source, nil).fields["event"]; got != componentJSON(event) {
				t.Fatalf("unexpected Event editor source: %s", got)
			}
			rendered, err := markdown(source)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(rendered), ">View map</a>") != (event.MapURL != "") ||
				strings.Contains(string(rendered), ">Event details</a>") != (event.URL != "") {
				t.Fatalf("unexpected Event link labels: %s", rendered)
			}
		})
	}
}
