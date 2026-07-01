package service

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// dateLayout is the calendar-date form accepted for window bounds.
const dateLayout = "2006-01-02"

// timeWindow is an inclusive [Since, Until] span. A nil bound is unbounded on
// that side.
type timeWindow struct {
	Since *time.Time
	Until *time.Time
}

// empty reports whether the window imposes no constraint.
func (w timeWindow) empty() bool { return w.Since == nil && w.Until == nil }

// startOfDay returns midnight at the start of t's calendar day, in t's location.
func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// endOfDay returns the last instant of t's calendar day, in t's location.
func endOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 23, 59, 59, int(time.Second-time.Nanosecond), t.Location())
}

// parseWindow resolves the date inputs into an inclusive [Since, Until] span,
// evaluating relative phrases ("last week", "yesterday") against now. Explicit
// since/until bounds take precedence; when neither is set, rangeExpr is
// consulted; a single date on any of them collapses to that whole day.
//
// Accepted forms for since/until: a calendar date (2006-01-02), an RFC3339
// timestamp, or "today"/"yesterday". rangeExpr additionally accepts spans like
// "last week", "past 2 days", "last 3 weeks", "this week", "last month".
func parseWindow(since, until, rangeExpr string, now time.Time) (timeWindow, error) {
	since, until, rangeExpr = strings.TrimSpace(since), strings.TrimSpace(until), strings.TrimSpace(rangeExpr)

	if since == "" && until == "" && rangeExpr != "" {
		return parseRange(rangeExpr, now)
	}

	var w timeWindow
	if since != "" {
		t, err := parseDayBound(since, now, false)
		if err != nil {
			return timeWindow{}, fmt.Errorf("since: %w", err)
		}
		w.Since = &t
	}
	if until != "" {
		t, err := parseDayBound(until, now, true)
		if err != nil {
			return timeWindow{}, fmt.Errorf("until: %w", err)
		}
		w.Until = &t
	}
	// A lone since or until naming a single day should scope to that one day.
	if w.Since != nil && w.Until == nil && isBareDate(since) {
		e := endOfDay(*w.Since)
		w.Until = &e
	}
	if w.Until != nil && w.Since == nil && isBareDate(until) {
		s := startOfDay(*w.Until)
		w.Since = &s
	}
	if w.Since != nil && w.Until != nil && w.Since.After(*w.Until) {
		w.Since, w.Until = w.Until, w.Since
	}
	return w, nil
}

// isBareDate reports whether s is a plain calendar date or a relative day word,
// as opposed to a full timestamp.
func isBareDate(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "today", "yesterday":
		return true
	}
	_, err := time.ParseInLocation(dateLayout, strings.TrimSpace(s), time.Local)
	return err == nil
}

// parseDayBound parses a single bound. When end is true a bare date resolves to
// the end of that day, otherwise to its start; an RFC3339 timestamp is taken
// verbatim.
func parseDayBound(s string, now time.Time, end bool) (time.Time, error) {
	switch strings.ToLower(s) {
	case "today":
		if end {
			return endOfDay(now), nil
		}
		return startOfDay(now), nil
	case "yesterday":
		y := now.AddDate(0, 0, -1)
		if end {
			return endOfDay(y), nil
		}
		return startOfDay(y), nil
	}
	if d, err := time.ParseInLocation(dateLayout, s, now.Location()); err == nil {
		if end {
			return endOfDay(d), nil
		}
		return startOfDay(d), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unrecognized date %q (use YYYY-MM-DD, RFC3339, today, or yesterday)", s)
}

var relSpanRe = regexp.MustCompile(`^(?:last|past|previous)?\s*(\d+)?\s*(day|week|month|year)s?$`)

// parseRange resolves a relative span or single date into an inclusive window
// ending at the end of today.
func parseRange(expr string, now time.Time) (timeWindow, error) {
	e := strings.ToLower(strings.TrimSpace(expr))

	switch e {
	case "today":
		s, u := startOfDay(now), endOfDay(now)
		return timeWindow{Since: &s, Until: &u}, nil
	case "yesterday":
		y := now.AddDate(0, 0, -1)
		s, u := startOfDay(y), endOfDay(y)
		return timeWindow{Since: &s, Until: &u}, nil
	case "this week":
		s := startOfWeek(now)
		u := endOfDay(now)
		return timeWindow{Since: &s, Until: &u}, nil
	case "this month":
		y, m, _ := now.Date()
		s := time.Date(y, m, 1, 0, 0, 0, 0, now.Location())
		u := endOfDay(now)
		return timeWindow{Since: &s, Until: &u}, nil
	}

	// "last week", "past 2 days", "last 3 weeks", "last month", ...
	if m := relSpanRe.FindStringSubmatch(e); m != nil {
		n := 1
		if m[1] != "" {
			var err error
			if n, err = strconv.Atoi(m[1]); err != nil || n < 1 {
				return timeWindow{}, fmt.Errorf("invalid count in %q", expr)
			}
		}
		until := endOfDay(now)
		var start time.Time
		switch m[2] {
		case "day":
			start = startOfDay(now.AddDate(0, 0, -(n - 1)))
		case "week":
			start = startOfDay(now.AddDate(0, 0, -(7*n - 1)))
		case "month":
			start = startOfDay(now.AddDate(0, -n, 0))
		case "year":
			start = startOfDay(now.AddDate(-n, 0, 0))
		}
		return timeWindow{Since: &start, Until: &until}, nil
	}

	// Fall back to a single calendar day.
	if d, err := time.ParseInLocation(dateLayout, e, now.Location()); err == nil {
		s, u := startOfDay(d), endOfDay(d)
		return timeWindow{Since: &s, Until: &u}, nil
	}
	if t, err := time.Parse(time.RFC3339, expr); err == nil {
		s, u := startOfDay(t), endOfDay(t)
		return timeWindow{Since: &s, Until: &u}, nil
	}

	return timeWindow{}, fmt.Errorf("unrecognized range %q (try a date, \"today\", \"yesterday\", \"last week\", or \"last 2 days\")", expr)
}

// startOfWeek returns midnight on the Monday of t's week.
func startOfWeek(t time.Time) time.Time {
	// Go's Weekday has Sunday=0; shift so Monday starts the week.
	offset := (int(t.Weekday()) + 6) % 7
	return startOfDay(t.AddDate(0, 0, -offset))
}
