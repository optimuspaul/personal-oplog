package service

import (
	"testing"
	"time"
)

// now is a Tuesday afternoon, so week math has a non-trivial offset.
var windowNow = time.Date(2026, 6, 23, 14, 30, 0, 0, time.UTC)

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestParseWindow_Range(t *testing.T) {
	cases := []struct {
		expr        string
		since, until time.Time
	}{
		{"today", day(2026, 6, 23), endOfDay(day(2026, 6, 23))},
		{"yesterday", day(2026, 6, 22), endOfDay(day(2026, 6, 22))},
		{"last 2 days", day(2026, 6, 22), endOfDay(day(2026, 6, 23))},
		{"last 3 days", day(2026, 6, 21), endOfDay(day(2026, 6, 23))},
		{"last week", day(2026, 6, 17), endOfDay(day(2026, 6, 23))},   // 7 days inclusive
		{"last 2 weeks", day(2026, 6, 10), endOfDay(day(2026, 6, 23))}, // 14 days inclusive
		{"this week", day(2026, 6, 22), endOfDay(day(2026, 6, 23))},    // Monday-of-week..today
		{"last month", day(2026, 5, 23), endOfDay(day(2026, 6, 23))},
		{"past 2 days", day(2026, 6, 22), endOfDay(day(2026, 6, 23))},
		{"2026-06-01", day(2026, 6, 1), endOfDay(day(2026, 6, 1))}, // bare date => that whole day
	}
	for _, c := range cases {
		w, err := parseWindow("", "", c.expr, windowNow)
		if err != nil {
			t.Fatalf("parseWindow(%q): %v", c.expr, err)
		}
		if w.Since == nil || !w.Since.Equal(c.since) {
			t.Errorf("%q: since = %v, want %v", c.expr, w.Since, c.since)
		}
		if w.Until == nil || !w.Until.Equal(c.until) {
			t.Errorf("%q: until = %v, want %v", c.expr, w.Until, c.until)
		}
	}
}

func TestParseWindow_SinceUntil(t *testing.T) {
	// Explicit inclusive range across days.
	w, err := parseWindow("2026-06-10", "2026-06-12", "", windowNow)
	if err != nil {
		t.Fatalf("parseWindow: %v", err)
	}
	if !w.Since.Equal(day(2026, 6, 10)) {
		t.Errorf("since = %v, want start of 06-10", w.Since)
	}
	if !w.Until.Equal(endOfDay(day(2026, 6, 12))) {
		t.Errorf("until = %v, want end of 06-12", w.Until)
	}
}

func TestParseWindow_LoneDateCollapsesToOneDay(t *testing.T) {
	// A single since date with no until should scope to that day, not open-ended.
	w, err := parseWindow("2026-06-10", "", "", windowNow)
	if err != nil {
		t.Fatalf("parseWindow: %v", err)
	}
	if w.Until == nil || !w.Until.Equal(endOfDay(day(2026, 6, 10))) {
		t.Errorf("lone since date should bound until to same day, got %v", w.Until)
	}
}

func TestParseWindow_TodayYesterdayBounds(t *testing.T) {
	w, err := parseWindow("yesterday", "today", "", windowNow)
	if err != nil {
		t.Fatalf("parseWindow: %v", err)
	}
	if !w.Since.Equal(day(2026, 6, 22)) || !w.Until.Equal(endOfDay(day(2026, 6, 23))) {
		t.Errorf("got [%v, %v]", w.Since, w.Until)
	}
}

func TestParseWindow_SwapsReversedBounds(t *testing.T) {
	w, err := parseWindow("2026-06-12", "2026-06-10", "", windowNow)
	if err != nil {
		t.Fatalf("parseWindow: %v", err)
	}
	if w.Since.After(*w.Until) {
		t.Errorf("expected bounds to be ordered, got since %v after until %v", w.Since, w.Until)
	}
}

func TestParseWindow_Empty(t *testing.T) {
	w, err := parseWindow("", "", "", windowNow)
	if err != nil {
		t.Fatalf("parseWindow: %v", err)
	}
	if !w.empty() {
		t.Error("expected empty window when no inputs given")
	}
}

func TestParseWindow_Invalid(t *testing.T) {
	if _, err := parseWindow("", "", "sometime last quarter", windowNow); err == nil {
		t.Error("expected error for unrecognized range")
	}
	if _, err := parseWindow("not-a-date", "", "", windowNow); err == nil {
		t.Error("expected error for unrecognized since")
	}
}
