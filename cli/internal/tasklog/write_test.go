package tasklog

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func mkProjectDir(t *testing.T, dataDir, slug string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dataDir, "projects", slug), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestLogSessionWritesWidgetCompatibleRow(t *testing.T) {
	dataDir := t.TempDir()
	mkProjectDir(t, dataDir, "acme")

	from := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 1, 10, 30, 0, 0, time.UTC)
	if err := LogSession(dataDir, "acme", `Fixed the "login" bug`, from, to, true); err != nil {
		t.Fatalf("LogSession: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dataDir, "projects", "acme", "task_log.csv"))
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected header + 1 data row, got %d lines: %q", len(lines), content)
	}
	if lines[0] != "uuid,task,from,to,completed" {
		t.Fatalf("unexpected header: %q", lines[0])
	}

	want := `^[0-9a-f-]{36},"Fixed the ""login"" bug",2026-09-01T09:00:00Z,2026-09-01T10:30:00Z,true$`
	if !regexp.MustCompile(want).MatchString(lines[1]) {
		t.Fatalf("row format mismatch:\n got:  %q\n want pattern: %q", lines[1], want)
	}
}

func TestLogSessionRefusesWhenLastRowIsOpen(t *testing.T) {
	dataDir := t.TempDir()
	mkProjectDir(t, dataDir, "acme")
	writeCSV(t, filepath.Join(dataDir, "projects", "acme", "task_log.csv"),
		`aaa,"Live session",2026-09-01T09:00:00Z,,`,
	)

	err := LogSession(dataDir, "acme", "Backdated entry",
		time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC),
		true)
	if err == nil {
		t.Fatal("expected LogSession to refuse writing over an open session, got nil error")
	}
}

func TestLogSessionAllowsWhenLastRowIsClosed(t *testing.T) {
	dataDir := t.TempDir()
	mkProjectDir(t, dataDir, "acme")
	writeCSV(t, filepath.Join(dataDir, "projects", "acme", "task_log.csv"),
		`aaa,"Earlier session",2026-09-01T09:00:00Z,2026-09-01T10:00:00Z,true`,
	)

	err := LogSession(dataDir, "acme", "Backdated entry",
		time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC),
		true)
	if err != nil {
		t.Fatalf("LogSession: %v", err)
	}

	rows, err := ReadRows(filepath.Join(dataDir, "projects", "acme", "task_log.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows after append, got %d", len(rows))
	}
}

func TestLogSessionRequiresToAfterFrom(t *testing.T) {
	dataDir := t.TempDir()
	mkProjectDir(t, dataDir, "acme")

	now := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	if err := LogSession(dataDir, "acme", "Bad range", now, now, true); err == nil {
		t.Fatal("expected error when to == from")
	}
	if err := LogSession(dataDir, "acme", "Bad range", now, now.Add(-time.Hour), true); err == nil {
		t.Fatal("expected error when to < from")
	}
}

func TestLogSessionRequiresExistingProjectDirectory(t *testing.T) {
	dataDir := t.TempDir()
	now := time.Now()
	err := LogSession(dataDir, "ghost", "task", now.Add(-time.Hour), now, true)
	if err == nil {
		t.Fatal("expected error for a project with no projects/<slug> directory")
	}
}

func TestNewUUIDLooksLikeUUIDv4(t *testing.T) {
	id := NewUUID()
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(id) {
		t.Fatalf("NewUUID() = %q, not a lowercase UUIDv4", id)
	}
}
