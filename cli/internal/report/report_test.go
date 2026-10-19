package report

import (
	"testing"
	"time"

	"github.com/ckritzinger/focus_on/cli/internal/tasklog"
)

func TestWeekStartIsMonday(t *testing.T) {
	sun := time.Date(2026, 10, 18, 23, 0, 0, 0, time.Local)
	if got := WeekStart(sun); !got.Equal(time.Date(2026, 10, 12, 0, 0, 0, 0, time.Local)) {
		t.Fatalf("sunday belongs to previous monday, got %v", got)
	}
	mon := time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)
	if got := WeekStart(mon); !got.Equal(mon) {
		t.Fatalf("monday is its own week start, got %v", got)
	}
}

func TestWeekHours(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.Local) // Wed
	end := func(h int) *time.Time { x := time.Date(2026, 10, 19, h, 0, 0, 0, time.Local); return &x }
	rows := []tasklog.Row{
		{From: time.Date(2026, 10, 19, 9, 0, 0, 0, time.Local), To: end(11)},                                                                                  // this week, 2h
		{From: time.Date(2026, 10, 18, 9, 0, 0, 0, time.Local), To: func() *time.Time { x := time.Date(2026, 10, 18, 10, 0, 0, 0, time.Local); return &x }()}, // last week Sun, 1h
		{From: time.Date(2026, 8, 1, 9, 0, 0, 0, time.Local), To: end(11)},                                                                                    // too old
	}
	_, hours := WeekHours(rows, now)
	if len(hours) != WeeksShown || hours[3] != 2 || hours[2] != 1 {
		t.Fatalf("unexpected hours %v", hours)
	}
}

func TestLatestPerSessionKeepsClosedRow(t *testing.T) {
	from := time.Date(2026, 10, 19, 9, 0, 0, 0, time.Local)
	to := from.Add(time.Hour)
	rows := []tasklog.Row{
		{UUID: "a", From: from},
		{UUID: "b", From: from},
		{UUID: "a", From: from, To: &to},
	}
	got := latestPerSession(rows)
	if len(got) != 2 || got[0].To == nil || got[1].To != nil {
		t.Fatalf("unexpected %+v", got)
	}
}
