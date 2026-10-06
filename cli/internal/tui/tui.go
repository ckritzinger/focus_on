// Package tui is the Bubble Tea app shell for the focuson CLI: first-run data
// directory setup, then the main menu. Invoices and Sync screens are wired in
// later phases — for now they're placeholders that just report "not yet
// implemented" and return to the menu.
package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ckritzinger/focus_on/cli/internal/config"
	"github.com/ckritzinger/focus_on/cli/internal/gitsync"
	"github.com/ckritzinger/focus_on/cli/internal/invoicing"
	"github.com/ckritzinger/focus_on/cli/internal/manifest"
)

type screen int

const (
	screenSetup screen = iota
	screenMenu
	screenBusinessForm
	screenClientsList
	screenClientForm
	screenProjectsList
	screenProjectClientPicker
	screenProjectForm
	screenInvoicesList
	screenInvoiceProjectPicker
	screenInvoiceDateBounds
	screenInvoiceReview
	screenInvoiceSetLast
	screenInvoiceDetail
	screenInvoiceRecon
	screenLogProjectPicker
	screenLogForm
	screenSync
	screenPlaceholder
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).MarginBottom(1)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)

var menuItems = []string{"Business", "Clients", "Projects", "Invoices", "Log Time", "Sync", "Quit"}

const addNewLabel = "+ Add new"

type Model struct {
	screen  screen
	cursor  int
	dataDir string
	err     error

	setupInput textinput.Model

	man     manifest.Manifest
	loadErr error

	businessForm form

	clientCursor      int
	editingClientSlug string // "" means adding a new client
	clientForm        form

	projectCursor       int
	editingProjectSlug  string           // "" means adding a new project
	draftProject        manifest.Project // carries Name/Slug/Rate across the client-picker step
	projectClientCursor int
	projectForm         form

	invoiceCursor         int
	invoiceList           []invoicing.Invoice
	invoiceListErr        error
	invoiceProjectSlugs   []string
	invoiceProjectCursor  int
	invoicePreviewProject string
	invoiceDateBoundsForm form
	invoiceOptions        invoicing.Options
	invoicePreview        invoicing.Invoice
	invoicePreviewErr     error
	invoiceCommitErr      error
	invoiceSetLastForm    form
	invoiceDetail         invoicing.Invoice
	invoiceReconIssues    []invoicing.ReconIssue
	invoiceReconErr       error

	logProjectSlugs  []string
	logProjectCursor int
	logProject       string
	logForm          form

	syncStatusLines []string
	syncErr         error
	syncDone        bool
	syncResult      gitsync.Result

	placeholderLabel string
}

// New builds the initial model. If cfg has no data dir yet, the app starts on
// the first-run setup screen; otherwise it bootstraps (idempotent) and goes
// straight to the main menu.
func New(cfg config.Config, hasCfg bool) Model {
	if hasCfg && cfg.DataDir != "" {
		return Model{screen: screenMenu, dataDir: cfg.DataDir}
	}

	suggestion, _ := config.DefaultDataDir()
	ti := textinput.New()
	ti.Placeholder = suggestion
	ti.Focus()
	ti.CharLimit = 512
	ti.Width = 60
	return Model{screen: screenSetup, setupInput: ti}
}

func (m Model) Init() tea.Cmd {
	if m.screen == screenSetup {
		return textinput.Blink
	}
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.screen {
		case screenSetup:
			return m.updateSetup(msg)
		case screenMenu:
			return m.updateMenu(msg)
		case screenBusinessForm:
			return m.updateBusinessForm(msg)
		case screenClientsList:
			return m.updateClientsList(msg)
		case screenClientForm:
			return m.updateClientForm(msg)
		case screenProjectsList:
			return m.updateProjectsList(msg)
		case screenProjectClientPicker:
			return m.updateProjectClientPicker(msg)
		case screenProjectForm:
			return m.updateProjectForm(msg)
		case screenInvoicesList:
			return m.updateInvoicesList(msg)
		case screenInvoiceProjectPicker:
			return m.updateInvoiceProjectPicker(msg)
		case screenInvoiceDateBounds:
			return m.updateInvoiceDateBounds(msg)
		case screenInvoiceReview:
			return m.updateInvoiceReview(msg)
		case screenInvoiceSetLast:
			return m.updateInvoiceSetLast(msg)
		case screenInvoiceDetail:
			return m.updateInvoiceDetail(msg)
		case screenInvoiceRecon:
			return m.updateInvoiceRecon(msg)
		case screenLogProjectPicker:
			return m.updateLogProjectPicker(msg)
		case screenLogForm:
			return m.updateLogForm(msg)
		case screenSync:
			return m.updateSync(msg)
		case screenPlaceholder:
			return m.updatePlaceholder(msg)
		}
	}
	return m, nil
}

func (m Model) updateSetup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC, tea.KeyEsc:
		return m, tea.Quit
	case tea.KeyEnter:
		dir := m.setupInput.Value()
		if dir == "" {
			dir = m.setupInput.Placeholder
		}
		if dir == "" {
			return m, nil
		}
		if err := manifest.Bootstrap(dir); err != nil {
			m.err = err
			return m, nil
		}
		if err := config.Save(config.Config{DataDir: dir}); err != nil {
			m.err = err
			return m, nil
		}
		m.dataDir = dir
		m.err = nil
		m.screen = screenMenu
		return m, nil
	}
	var cmd tea.Cmd
	m.setupInput, cmd = m.setupInput.Update(msg)
	return m, cmd
}

func (m Model) updateMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(menuItems)-1 {
			m.cursor++
		}
	case "enter":
		switch menuItems[m.cursor] {
		case "Quit":
			return m, tea.Quit
		case "Business":
			return m.enterBusinessForm()
		case "Clients":
			return m.enterClientsList()
		case "Projects":
			return m.enterProjectsList()
		case "Invoices":
			return m.enterInvoicesList()
		case "Log Time":
			return m.enterLogProjectPicker()
		case "Sync":
			return m.enterSync()
		default:
			m.placeholderLabel = menuItems[m.cursor]
			m.screen = screenPlaceholder
		}
	}
	return m, nil
}

func (m Model) updatePlaceholder(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	default:
		m.screen = screenMenu
	}
	return m, nil
}

// reloadManifest re-reads manifest.toml from disk. The CLI is the sole
// writer, but re-reading on every screen entry is cheap and keeps the
// in-memory copy honest if the file was ever hand-edited mid-session.
func (m *Model) reloadManifest() {
	man, err := manifest.Load(m.dataDir)
	m.man = man
	m.loadErr = err
}

func (m Model) View() string {
	switch m.screen {
	case screenSetup:
		return m.viewSetup()
	case screenMenu:
		return m.viewMenu()
	case screenBusinessForm:
		return m.businessForm.View()
	case screenClientsList:
		return m.viewClientsList()
	case screenClientForm:
		return m.clientForm.View()
	case screenProjectsList:
		return m.viewProjectsList()
	case screenProjectClientPicker:
		return m.viewProjectClientPicker()
	case screenProjectForm:
		return m.projectForm.View()
	case screenInvoicesList:
		return m.viewInvoicesList()
	case screenInvoiceProjectPicker:
		return m.viewInvoiceProjectPicker()
	case screenInvoiceDateBounds:
		return m.invoiceDateBoundsForm.View()
	case screenInvoiceReview:
		return m.viewInvoiceReview()
	case screenInvoiceSetLast:
		return m.invoiceSetLastForm.View()
	case screenInvoiceDetail:
		return m.viewInvoiceDetail()
	case screenInvoiceRecon:
		return m.viewInvoiceRecon()
	case screenLogProjectPicker:
		return m.viewLogProjectPicker()
	case screenLogForm:
		return m.logForm.View()
	case screenSync:
		return m.viewSync()
	case screenPlaceholder:
		return m.viewPlaceholder()
	}
	return ""
}

func (m Model) viewSetup() string {
	var b string
	b += titleStyle.Render("FocusOn — first-run setup") + "\n"
	b += "Where should your client/project/invoice data live?\n\n"
	b += m.setupInput.View() + "\n\n"
	if m.err != nil {
		b += errorStyle.Render(fmt.Sprintf("error: %v", m.err)) + "\n\n"
	}
	b += dimStyle.Render("enter to confirm · esc to quit")
	return b
}

func (m Model) viewMenu() string {
	return renderList("FocusOn", m.dataDir, menuItems, m.cursor, "↑/↓ to move · enter to select · q to quit")
}

func (m Model) viewPlaceholder() string {
	var b string
	b += titleStyle.Render(m.placeholderLabel) + "\n"
	b += "Not built yet.\n\n"
	b += dimStyle.Render("any key to go back")
	return b
}
