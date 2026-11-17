package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ckritzinger/focus_on/cli/internal/invoicing"
)

// Expenses: reimbursable costs recorded per project in expense_log.csv and
// picked up by the next invoice (spec_v2.md, "expense_log.csv"). The list
// shows only unbilled expenses; billed ones live on in their invoice's
// ledger. There's no edit/delete — fix a typo by hand-editing the CSV.

// --- Project picker --------------------------------------------------------

func (m Model) enterExpenseProjectPicker() (tea.Model, tea.Cmd) {
	m.reloadManifest()
	m.expenseProjectSlugs = m.billableProjectSlugs()
	m.expenseProjectCursor = 0
	m.screen = screenExpenseProjectPicker
	return m, nil
}

func (m Model) updateExpenseProjectPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.screen = screenMenu
	case "up", "k":
		if m.expenseProjectCursor > 0 {
			m.expenseProjectCursor--
		}
	case "down", "j":
		if m.expenseProjectCursor < len(m.expenseProjectSlugs)-1 {
			m.expenseProjectCursor++
		}
	case "enter":
		if len(m.expenseProjectSlugs) == 0 {
			return m, nil
		}
		m.expenseProject = m.expenseProjectSlugs[m.expenseProjectCursor]
		return m.enterExpensesList()
	}
	return m, nil
}

func (m Model) viewExpenseProjectPicker() string {
	if len(m.expenseProjectSlugs) == 0 {
		var b strings.Builder
		b.WriteString(titleStyle.Render("Expenses for which project?") + "\n")
		b.WriteString("No billable projects yet — add a client and project first.\n\n")
		b.WriteString(dimStyle.Render("esc back to menu"))
		return b.String()
	}
	return renderList("Expenses for which project?", "", m.expenseProjectSlugs, m.expenseProjectCursor,
		"↑/↓ to move · enter to continue · esc to cancel")
}

// --- Unbilled expenses list ------------------------------------------------

const expenseActionAdd = "+ Add expense"

func (m Model) enterExpensesList() (tea.Model, tea.Cmd) {
	m.expenseList, m.expenseListErr = invoicing.UnbilledExpenses(m.dataDir, m.expenseProject)
	m.expenseCursor = 0
	m.screen = screenExpensesList
	return m, nil
}

func (m Model) expenseListItems() []string {
	items := []string{expenseActionAdd}
	for _, e := range m.expenseList {
		items = append(items, e.Summary())
	}
	return items
}

func (m Model) updateExpensesList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.expenseListItems()
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.screen = screenExpenseProjectPicker
	case "up", "k":
		if m.expenseCursor > 0 {
			m.expenseCursor--
		}
	case "down", "j":
		if m.expenseCursor < len(items)-1 {
			m.expenseCursor++
		}
	case "enter":
		if m.expenseCursor == 0 {
			m.expenseForm = newExpenseForm()
			m.screen = screenExpenseForm
		}
	}
	return m, nil
}

func (m Model) viewExpensesList() string {
	var total float64
	for _, e := range m.expenseList {
		total += e.Amount
	}
	subtitle := fmt.Sprintf("%d unbilled, %.2f total — picked up by the next invoice", len(m.expenseList), total)
	if m.expenseListErr != nil {
		subtitle = "error loading expenses: " + m.expenseListErr.Error()
	}
	return renderList("Expenses: "+m.expenseProject, subtitle, m.expenseListItems(), m.expenseCursor,
		"↑/↓ to move · enter on “add” to record one · esc back · fix typos by editing expense_log.csv")
}

// --- Add expense form ------------------------------------------------------

const (
	expenseFieldDate = iota
	expenseFieldDesc
	expenseFieldAmount
	expenseFieldOriginal
	expenseFieldNoVAT
	expenseFieldReceipt
)

func newExpenseForm() form {
	return newForm("Add expense",
		[]string{
			"Date (YYYY-MM-DD)",
			"Description",
			"Amount (client currency, ex VAT)",
			`Original amount (optional, e.g. "67.89 USD")`,
			"No VAT — pure disbursement (y/n)",
			"Receipt path (optional)",
		},
		[]string{time.Now().Format(invoicing.ExpenseDateFormat), "", "", "", "n", ""},
		[]bool{false, false, false, false, false, false})
}

func (m Model) updateExpenseForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	f, cmd, submitted, cancelled := m.expenseForm.update(msg)
	m.expenseForm = f
	if cancelled {
		m.screen = screenExpensesList
		return m, nil
	}
	if !submitted {
		return m, cmd
	}

	vals := f.values()
	amount, err := strconv.ParseFloat(vals[expenseFieldAmount], 64)
	if err != nil {
		m.expenseForm.err = fmt.Errorf("amount: expected a number, got %q", vals[expenseFieldAmount])
		return m, cmd
	}
	origAmt, origCur, err := invoicing.ParseOriginal(vals[expenseFieldOriginal])
	if err != nil {
		m.expenseForm.err = fmt.Errorf("original amount: %v", err)
		return m, cmd
	}
	noVAT := strings.ToLower(vals[expenseFieldNoVAT])
	_, err = invoicing.AddExpense(m.dataDir, m.expenseProject, invoicing.Expense{
		Date:             vals[expenseFieldDate],
		Description:      vals[expenseFieldDesc],
		Amount:           amount,
		OriginalAmount:   origAmt,
		OriginalCurrency: origCur,
		NoVAT:            noVAT == "y" || noVAT == "yes",
		Receipt:          vals[expenseFieldReceipt],
	})
	if err != nil {
		m.expenseForm.err = err
		return m, cmd
	}
	return m.enterExpensesList()
}
