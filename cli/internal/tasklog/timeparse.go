package tasklog

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var relativeTimePattern = regexp.MustCompile(`^-(\d+)(h|m)$`)

// ParseTime parses the shorthand `focuson log` and the TUI's Log Time form
// accept for --from/--to: a bare "HH:MM" (today, local time), a full
// "YYYY-MM-DD HH:MM" (local time), a duration relative to now ("-2h",
// "-90m"), or — as a fallback — a full RFC3339 timestamp. Typing a correct
// RFC3339 timestamp by hand is exactly the kind of friction this command
// exists to avoid, so the shorthand forms are the primary interface.
func ParseTime(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}

	if m := relativeTimePattern.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid relative time %q", s)
		}
		unit := time.Minute
		if m[2] == "h" {
			unit = time.Hour
		}
		return now.Add(-time.Duration(n) * unit), nil
	}

	if t, err := time.ParseInLocation("15:04", s, time.Local); err == nil {
		return time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, time.Local), nil
	}

	if t, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local); err == nil {
		return t, nil
	}

	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("unrecognized time %q — use HH:MM, \"YYYY-MM-DD HH:MM\", -Nh/-Nm relative to now, or RFC3339", s)
}
