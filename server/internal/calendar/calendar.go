package calendar

import "time"

var China = time.FixedZone("CST", 8*60*60)

// Today returns the China calendar date as a UTC date-only value.
func Today(t time.Time) time.Time {
	t = t.In(China)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
