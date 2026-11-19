package blog

import (
	"errors"
	"fmt"
	"time"
	_ "time/tzdata" // Resolve time zones on hosts without a zoneinfo database.
)

const localTimeLayout = "2006-01-02 15:04"

var errRepeatedTime = errors.New("This time occurs twice when clocks change.")

func loadTimeZone(name string) (*time.Location, error) {
	if name == "" || name == "Local" || len(name) > 80 {
		return nil, fmt.Errorf("invalid time zone %q", name)
	}
	return time.LoadLocation(name)
}

func (m *module) siteTimeZone() *time.Location {
	if m.location == nil {
		return time.UTC
	}
	return m.location
}

func parseLocalTime(value string, zone *time.Location) (time.Time, error) {
	at, err := time.ParseInLocation(localTimeLayout, value, zone)
	if err != nil || at.Format(localTimeLayout) != value {
		return time.Time{}, contentError("Choose a valid local time. Some times are skipped when clocks change.")
	}
	if repeatedLocalTime(at) {
		return time.Time{}, errRepeatedTime
	}
	return at, nil
}

// The other offset from either adjacent interval can map the same clock time
// to another instant when the clock moves backward.
func repeatedLocalTime(at time.Time) bool {
	since, until := at.ZoneBounds()
	_, offset := at.Zone()
	value := at.Format(localTimeLayout)
	for _, adjacent := range []time.Time{since.Add(-time.Second), until} {
		_, otherOffset := adjacent.Zone()
		if otherOffset != offset && at.Add(time.Duration(offset-otherOffset)*time.Second).Format(localTimeLayout) == value {
			return true
		}
	}
	return false
}
