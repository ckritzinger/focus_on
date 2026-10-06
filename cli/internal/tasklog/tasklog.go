// Package tasklog reads (and, via `focuson log`, writes) task_log.csv — the
// append-only log of task sessions (see spec_v2.md, "CSV Schema"). Writing
// was historically widget-only; LogSession in write.go is the one
// deliberate exception, added as a flake-resistant alternative to the
// widget's own manual-entry form, and it stays format- and
// invariant-compatible with the widget's own writer (see write.go).
package tasklog

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"time"
)

// Row is one line of task_log.csv. Line is its 1-indexed position in the
// file including the header (so the first data row is Line 2), which makes
// error messages point a human straight at the offending line.
type Row struct {
	Line      int
	UUID      string
	Task      string
	From      time.Time
	To        *time.Time
	Completed *bool
}

const expectedFields = 5

// ReadRows parses a project's task_log.csv. A missing file is not an error —
// it just means the project has no logged time yet — but a malformed row
// (wrong column count, unparseable timestamp) is: an integrity checker that
// silently skipped rows it couldn't parse would defeat its own purpose.
func ReadRows(path string) ([]Row, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	return parseRows(f, path)
}

// parseRows does the actual parsing, over any io.Reader positioned at the
// start of a task_log.csv — split out of ReadRows so LogSession (write.go)
// can read the current contents of an already-open, already-locked file
// without a second, racy open-by-path.
func parseRows(r io.Reader, path string) ([]Row, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1

	if _, err := cr.Read(); err != nil { // header
		if err == io.EOF {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s header: %w", path, err)
	}

	var rows []Row
	line := 1
	for {
		fields, err := cr.Read()
		line++
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		if len(fields) != expectedFields {
			return nil, fmt.Errorf("%s:%d: expected %d fields, got %d", path, line, expectedFields, len(fields))
		}
		from, err := time.Parse(time.RFC3339, fields[2])
		if err != nil {
			return nil, fmt.Errorf("%s:%d: invalid from timestamp %q: %w", path, line, fields[2], err)
		}
		var to *time.Time
		if fields[3] != "" {
			t, err := time.Parse(time.RFC3339, fields[3])
			if err != nil {
				return nil, fmt.Errorf("%s:%d: invalid to timestamp %q: %w", path, line, fields[3], err)
			}
			to = &t
		}
		var completed *bool
		if fields[4] != "" {
			b := fields[4] == "true"
			completed = &b
		}
		rows = append(rows, Row{
			Line:      line,
			UUID:      fields[0],
			Task:      fields[1],
			From:      from,
			To:        to,
			Completed: completed,
		})
	}
	return rows, nil
}
