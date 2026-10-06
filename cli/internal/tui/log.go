package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ckritzinger/focus_on/cli/internal/tasklog"
)

// --- Project picker --------------------------------------------------------

func (m Model) enterLogProjectPicker() (tea.Model, tea.Cmd) {
	m.reloadManifest()
	m.logProjectSlugs = nil
	for _, p := range m.man.Projects {
		m.logProjectSlugs = append(m.logProjectSlugs, p.Slug)
	}
	m.logProjectCursor = 0
	m.screen = screenLogProjectPicker
	return m, nil
}

func (m Model) updateLogProjectPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.screen = screenMenu
	case "up", "k":
		if m.logProjectCursor > 0 {
			m.logProjectCursor--
		}
	case "down", "j":
		if m.logProjectCursor < len(m.logProjectSlugs)-1 {
			m.logProjectCursor++
		}
	case "enter":
		if len(m.logProjectSlugs) == 0 {
			return m, nil
		}
		m.logProject = m.logProjectSlugs[m.logProjectCursor]
		m.logForm = newLogForm()
		m.screen = screenLogForm
	}
	return m, nil
}

func (m Model) viewLogProjectPicker() string {
	if len(m.logProjectSlugs) == 0 {
		var b strings.Builder
		b.WriteString(titleStyle.Render("Log time for which project?") + "\n")
		b.WriteString("No projects yet — add one first.\n\n")
		b.WriteString(dimStyle.Render("esc back to menu"))
		return b.String()
	}
	return renderList("Log time for which project?", "", m.logProjectSlugs, m.logProjectCursor,
		"↑/↓ to move · enter to continue · esc to cancel")
}

// --- Task/from/to/completed form -------------------------------------------

const (
	logFieldTask = iota
	logFieldFrom
	logFieldTo
	logFieldCompleted
)

func newLogForm() form {
	return newForm("Log past session",
		[]string{
			"Task",
			`From (HH:MM, "YYYY-MM-DD HH:MM", or -2h/-90m ago)`,
			"To (blank = now)",
			"Completed (y/n)",
		},
		[]string{"", "", "", "y"},
		[]bool{false, false, false, false})
}

func (m Model) updateLogForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	f, cmd, submitted, cancelled := m.logForm.update(msg)
	m.logForm = f
	if cancelled {
		m.screen = screenLogProjectPicker
		return m, nil
	}
	if !submitted {
		return m, cmd
	}

	vals := f.values()
	task := vals[logFieldTask]
	if task == "" {
		m.logForm.err = fmt.Errorf("task is required")
		return m, cmd
	}
	if vals[logFieldFrom] == "" {
		m.logForm.err = fmt.Errorf("from is required")
		return m, cmd
	}

	now := time.Now()
	from, err := tasklog.ParseTime(vals[logFieldFrom], now)
	if err != nil {
		m.logForm.err = fmt.Errorf("from: %v", err)
		return m, cmd
	}
	to := now
	if vals[logFieldTo] != "" {
		to, err = tasklog.ParseTime(vals[logFieldTo], now)
		if err != nil {
			m.logForm.err = fmt.Errorf("to: %v", err)
			return m, cmd
		}
	}
	completedStr := strings.ToLower(vals[logFieldCompleted])
	completed := completedStr != "n" && completedStr != "no"

	if err := tasklog.LogSession(m.dataDir, m.logProject, task, from, to, completed); err != nil {
		m.logForm.err = err
		return m, cmd
	}
	m.screen = screenMenu
	return m, nil
}
