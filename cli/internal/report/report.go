// Package report builds the `focuson report` summary: weekly hour totals,
// uninvoiced work, and a session-level task list for the last two weeks.
// Weeks run Monday to Sunday in local time; a session belongs to the week
// it started in.
package report

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"time"

	"github.com/ckritzinger/focus_on/cli/internal/invoicing"
	"github.com/ckritzinger/focus_on/cli/internal/manifest"
	"github.com/ckritzinger/focus_on/cli/internal/tasklog"
)

// WeeksShown is how many weeks (including the current one) get an hours total.
const WeeksShown = 4

// WeekStart returns midnight local time on the Monday of t's week.
func WeekStart(t time.Time) time.Time {
	t = t.In(time.Local)
	offset := (int(t.Weekday()) + 6) % 7 // Monday=0 ... Sunday=6
	return time.Date(t.Year(), t.Month(), t.Day()-offset, 0, 0, 0, 0, time.Local)
}

func duration(r tasklog.Row, now time.Time) time.Duration {
	if r.To == nil {
		return now.Sub(r.From)
	}
	return r.To.Sub(r.From)
}

// WeekHours returns total hours per week, oldest first, for the WeeksShown
// weeks ending with the week containing now. Open sessions count up to now.
func WeekHours(rows []tasklog.Row, now time.Time) (starts []time.Time, hours []float64) {
	cur := WeekStart(now)
	for i := WeeksShown - 1; i >= 0; i-- {
		starts = append(starts, cur.AddDate(0, 0, -7*i))
	}
	hours = make([]float64, len(starts))
	for _, r := range rows {
		ws := WeekStart(r.From)
		for i, s := range starts {
			if s.Equal(ws) {
				hours[i] += duration(r, now).Hours()
			}
		}
	}
	return starts, hours
}

func status(r tasklog.Row) string {
	switch {
	case r.To == nil:
		return "in progress"
	case r.Completed != nil && *r.Completed:
		return "completed"
	default:
		return "not completed"
	}
}

// Print writes the full report for one project.
func Print(w io.Writer, dataDir string, man manifest.Manifest, slug string, now time.Time) error {
	project, ok := man.FindProject(slug)
	if !ok {
		return fmt.Errorf("unknown project %q", slug)
	}
	rows, err := tasklog.ReadRows(filepath.Join(dataDir, "projects", slug, "task_log.csv"))
	if err != nil {
		return err
	}

	fmt.Fprintf(w, "Report: %s (%s)\n\n", project.Name, slug)

	fmt.Fprintln(w, "Hours per week")
	starts, hours := WeekHours(rows, now)
	for i, s := range starts {
		note := ""
		if i == len(starts)-1 {
			note = "  (this week)"
		}
		fmt.Fprintf(w, "  %s - %s  %6.2f h%s\n", s.Format("2006-01-02"), s.AddDate(0, 0, 6).Format("2006-01-02"), hours[i], note)
	}

	fmt.Fprintln(w, "\nUninvoiced")
	inv, err := invoicing.Preview(dataDir, man, invoicing.Options{ProjectSlug: slug})
	if err != nil {
		fmt.Fprintf(w, "  n/a: %v\n", err)
	} else {
		var clock time.Duration
		for _, li := range inv.LineItems {
			clock += li.To.Sub(li.From)
		}
		fmt.Fprintf(w, "  %.2f h  %.2f %s", clock.Hours(), inv.TotalAmount, inv.Currency)
		if inv.VATPercent > 0 {
			fmt.Fprintf(w, " (incl VAT)")
		}
		fmt.Fprintln(w)
	}

	cur := WeekStart(now)
	for _, weekStart := range []time.Time{cur.AddDate(0, 0, -7), cur} {
		label := "Last week"
		if weekStart.Equal(cur) {
			label = "This week"
		}
		fmt.Fprintf(w, "\n%s (%s - %s)\n", label, weekStart.Format("2006-01-02"), weekStart.AddDate(0, 0, 6).Format("2006-01-02"))
		var week []tasklog.Row
		for _, r := range rows {
			if WeekStart(r.From).Equal(weekStart) {
				week = append(week, r)
			}
		}
		sort.SliceStable(week, func(i, j int) bool { return week[i].From.Before(week[j].From) })
		if len(week) == 0 {
			fmt.Fprintln(w, "  no tasks tracked")
		}
		for _, r := range week {
			to := "now"
			if r.To != nil {
				to = r.To.In(time.Local).Format("15:04")
			}
			fmt.Fprintf(w, "  %s %s-%s  %5.2f h  [%s]  %s\n",
				r.From.In(time.Local).Format("Mon 01-02"), r.From.In(time.Local).Format("15:04"), to,
				duration(r, now).Hours(), status(r), r.Task)
		}
	}
	return nil
}
