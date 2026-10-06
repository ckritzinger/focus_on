package tasklog

import (
	"testing"
	"time"
)

func TestParseTimeRelative(t *testing.T) {
	now := time.Date(2026, 9, 1, 14, 30, 0, 0, time.Local)

	got, err := ParseTime("-2h", now)
	if err != nil {
		t.Fatalf("ParseTime(-2h): %v", err)
	}
	if want := now.Add(-2 * time.Hour); !got.Equal(want) {
		t.Fatalf("ParseTime(-2h) = %v, want %v", got, want)
	}

	got, err = ParseTime("-90m", now)
	if err != nil {
		t.Fatalf("ParseTime(-90m): %v", err)
	}
	if want := now.Add(-90 * time.Minute); !got.Equal(want) {
		t.Fatalf("ParseTime(-90m) = %v, want %v", got, want)
	}
}

func TestParseTimeBareHHMMAnchorsToday(t *testing.T) {
	now := time.Date(2026, 9, 1, 14, 30, 0, 0, time.Local)
	got, err := ParseTime("09:15", now)
	if err != nil {
		t.Fatalf("ParseTime(09:15): %v", err)
	}
	want := time.Date(2026, 9, 1, 9, 15, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("ParseTime(09:15) = %v, want %v", got, want)
	}
}

func TestParseTimeFullDateTime(t *testing.T) {
	now := time.Date(2026, 9, 1, 14, 30, 0, 0, time.Local)
	got, err := ParseTime("2026-08-15 08:00", now)
	if err != nil {
		t.Fatalf("ParseTime: %v", err)
	}
	want := time.Date(2026, 8, 15, 8, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseTimeRFC3339Fallback(t *testing.T) {
	now := time.Now()
	got, err := ParseTime("2026-08-15T08:00:00-07:00", now)
	if err != nil {
		t.Fatalf("ParseTime: %v", err)
	}
	want, _ := time.Parse(time.RFC3339, "2026-08-15T08:00:00-07:00")
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseTimeRejectsGarbage(t *testing.T) {
	if _, err := ParseTime("not a time", time.Now()); err == nil {
		t.Fatal("expected an error for unparseable input")
	}
	if _, err := ParseTime("", time.Now()); err == nil {
		t.Fatal("expected an error for empty input")
	}
}
