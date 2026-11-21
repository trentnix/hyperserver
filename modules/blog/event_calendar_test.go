package blog

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestEventCalendarSchedule(t *testing.T) {
	issued := time.Date(2026, 11, 21, 10, 15, 30, 0, time.FixedZone("CST", -6*60*60))
	for _, tc := range []struct {
		name  string
		event postEvent
		want  []string
	}{
		{"timed across a clock change", postEvent{Title: "Overnight event", StartDate: "2027-03-13", StartTime: "22:00", EndDate: "2027-03-14", EndTime: "04:00", TimeZone: "America/Chicago"},
			[]string{"DTSTART:20270314T040000Z", "DTEND:20270314T090000Z"}},
		{"timed without an end", postEvent{Title: "Talk", StartDate: "2027-01-12", StartTime: "18:30", TimeZone: "America/Chicago"},
			[]string{"DTSTART:20270113T003000Z"}},
		{"all-day range", postEvent{Title: "Open studio", StartDate: "2027-04-30", EndDate: "2027-05-02"},
			[]string{"DTSTART;VALUE=DATE:20270430", "DTEND;VALUE=DATE:20270503"}},
		{"single all-day event", postEvent{Title: "Open studio", StartDate: "2027-04-16"},
			[]string{"DTSTART;VALUE=DATE:20270416", "DTEND;VALUE=DATE:20270417"}},
		{"all-day range at the calendar limit", postEvent{Title: "Open studio", StartDate: "9999-12-30", EndDate: "9999-12-31"},
			[]string{"DTSTART;VALUE=DATE:99991230", "DURATION:P2D"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event, err := parseEvent(componentJSON(tc.event))
			if err != nil {
				t.Fatal(err)
			}
			calendar := event.calendar(issued)
			if !strings.HasPrefix(calendar, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:") ||
				!strings.HasSuffix(calendar, "END:VEVENT\r\nEND:VCALENDAR\r\n") ||
				!strings.Contains(calendar, "\r\nDTSTAMP:20261121T161530Z\r\n") {
				t.Fatalf("invalid calendar envelope or timestamp: %q", calendar)
			}
			for _, line := range tc.want {
				if !strings.Contains(calendar, "\r\n"+line+"\r\n") {
					t.Errorf("missing %q in %q", line, calendar)
				}
			}
			if tc.name == "timed without an end" && strings.Contains(calendar, "\r\nDTEND") {
				t.Errorf("calendar invented an end time: %q", calendar)
			}
			_, uid, found := strings.Cut(calendar, "\r\nUID:")
			uid, _, _ = strings.Cut(uid, "\r\n")
			if !found || uid == "" || !strings.Contains(event.calendar(issued.Add(time.Hour)), "\r\nUID:"+uid+"\r\n") {
				t.Error("repeat downloads must use the same event ID")
			}
		})
	}
}

func TestEventCalendarText(t *testing.T) {
	name := strings.Repeat("月", 30) + " café"
	event, err := parseEvent(componentJSON(postEvent{
		Title: name + ", hall; wing\\2\r\nATTENDEE:example", StartDate: "2027-04-16",
		Location: "Room\x00 A, floor; 2", URL: "https://example.com/event?one=1&two=2",
	}))
	if err != nil {
		t.Fatal(err)
	}
	calendar := event.calendar(time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC))
	if !strings.Contains(calendar, "\r\n ") {
		t.Fatal("the long title must fold across content lines")
	}
	for line := range strings.SplitSeq(calendar, "\r\n") {
		if len(line) > 75 || !utf8.ValidString(line) || strings.ContainsAny(line, "\r\n\x00") {
			t.Fatalf("invalid content line: %q", line)
		}
	}
	unfolded := strings.ReplaceAll(calendar, "\r\n ", "")
	for _, want := range []string{
		"SUMMARY:" + name + `\, hall\; wing\\2\nATTENDEE:example`,
		`LOCATION:Room  A\, floor\; 2`,
		`URL:https://example.com/event?one=1&two=2`,
	} {
		if !strings.Contains(unfolded, "\r\n"+want+"\r\n") {
			t.Errorf("missing %q in %q", want, unfolded)
		}
	}
}
