package tasklog

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const csvHeader = "uuid,task,from,to,completed\n"

// NewUUID returns a lowercase UUIDv4, matching the widget's own
// UUID().uuidString.lowercased() — session UUIDs are meaningless across the
// two sides otherwise.
func NewUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("tasklog: reading crypto/rand: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// formatRow renders one row exactly as the widget's CSVLogger.appendRow
// does: task double-quoted with `"` doubled (no other escaping), from/to as
// RFC3339 in UTC (ISO8601DateFormatter's default timeZone), completed as
// true/false/empty. Any divergence here would make a row this package
// writes unreadable as "the same format" to the widget or to ReadRows.
func formatRow(row Row) string {
	escapedTask := strings.ReplaceAll(row.Task, `"`, `""`)
	toStr := ""
	if row.To != nil {
		toStr = row.To.UTC().Format(time.RFC3339)
	}
	completedStr := ""
	if row.Completed != nil {
		if *row.Completed {
			completedStr = "true"
		} else {
			completedStr = "false"
		}
	}
	return fmt.Sprintf("%s,\"%s\",%s,%s,%s\n", row.UUID, escapedTask, row.From.UTC().Format(time.RFC3339), toStr, completedStr)
}

// LogSession appends a single, already-closed session row — the CLI
// equivalent of the widget's "Log past session" action (spec_v2.md), for
// backdating time without the flaky SwiftUI date-picker form. project must
// already have a projects/<slug> directory (callers are expected to have
// validated the slug against manifest.toml first; this package doesn't
// depend on manifest, so it only checks the directory).
//
// Refuses to write if the project's last row is still open (no `to`) — that
// row is the one CheckDataDirectory trusts to mean "a session is actively
// being tracked right now" (integrity.go), and appending past it would make
// the next startup's integrity check wrongly treat a real open session as
// abandoned. The read-then-write happens under one flock, so two concurrent
// `focuson log` runs can't both pass the check and both append; this does
// not protect against the widget's own unlocked append landing mid-write.
func LogSession(dataDir, project, task string, from, to time.Time, completed bool) error {
	if !to.After(from) {
		return fmt.Errorf("to (%s) must be after from (%s)", to.Format("2006-01-02 15:04"), from.Format("2006-01-02 15:04"))
	}

	projectDir := filepath.Join(dataDir, "projects", project)
	if info, err := os.Stat(projectDir); err != nil || !info.IsDir() {
		return fmt.Errorf("no projects/%s directory — add the project first", project)
	}
	path := filepath.Join(projectDir, "task_log.csv")

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("locking %s: %w", path, err)
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN)

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}

	rows, err := parseRows(f, path)
	if err != nil {
		return err
	}
	if n := len(rows); n > 0 && rows[n-1].To == nil {
		last := rows[n-1]
		return fmt.Errorf("project %q has an open session (uuid %s, started %s) — close it from the widget before logging another", project, last.UUID, last.From.Format("2006-01-02 15:04"))
	}

	if info.Size() == 0 {
		if _, err := f.WriteString(csvHeader); err != nil {
			return fmt.Errorf("writing header to %s: %w", path, err)
		}
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("seeking %s: %w", path, err)
	}
	row := Row{UUID: NewUUID(), Task: task, From: from, To: &to, Completed: &completed}
	if _, err := f.WriteString(formatRow(row)); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
