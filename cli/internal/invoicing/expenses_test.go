package invoicing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ckritzinger/focus_on/cli/internal/manifest"
)

func mustAddExpense(t *testing.T, dataDir string, e Expense) Expense {
	t.Helper()
	got, err := AddExpense(dataDir, "acme-website", e)
	if err != nil {
		t.Fatalf("AddExpense: %v", err)
	}
	return got
}

func TestAddExpenseRoundTrips(t *testing.T) {
	dataDir, _ := setupProject(t)
	want := Expense{
		Date:             "2026-10-31",
		Description:      `OpenRouter, "October"`,
		Amount:           1234.5,
		OriginalAmount:   67.89,
		OriginalCurrency: "USD",
		NoVAT:            true,
		Receipt:          "receipts/openrouter-oct.pdf",
	}
	added := mustAddExpense(t, dataDir, want)
	if added.UUID == "" {
		t.Fatal("expected a UUID to be assigned")
	}
	mustAddExpense(t, dataDir, Expense{Date: "2026-11-01", Description: "Second", Amount: 10})

	got, err := ReadExpenses(dataDir, "acme-website")
	if err != nil {
		t.Fatalf("ReadExpenses: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 expenses, got %d", len(got))
	}
	want.UUID = added.UUID
	if got[0] != want {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got[0], want)
	}

	raw, _ := os.ReadFile(filepath.Join(dataDir, "projects", "acme-website", "expense_log.csv"))
	if strings.Count(string(raw), "uuid,date,description") != 1 {
		t.Fatalf("expected exactly one header line, file:\n%s", raw)
	}
}

func TestAddExpenseValidates(t *testing.T) {
	dataDir, _ := setupProject(t)
	cases := []Expense{
		{Date: "2026-10-31", Description: "", Amount: 10},
		{Date: "31/10/2026", Description: "x", Amount: 10},
		{Date: "2026-10-31", Description: "x", Amount: 0},
		{Date: "2026-10-31", Description: "x", Amount: -5},
	}
	for _, e := range cases {
		if _, err := AddExpense(dataDir, "acme-website", e); err == nil {
			t.Errorf("expected %+v to be rejected", e)
		}
	}
	if _, err := AddExpense(dataDir, "no-such-project", Expense{Date: "2026-10-31", Description: "x", Amount: 1}); err == nil {
		t.Error("expected unknown project to be rejected")
	}
}

func TestParseOriginal(t *testing.T) {
	amt, cur, err := ParseOriginal("67.89 usd")
	if err != nil || amt != 67.89 || cur != "USD" {
		t.Fatalf("got %v %q %v", amt, cur, err)
	}
	if amt, cur, err := ParseOriginal(""); err != nil || amt != 0 || cur != "" {
		t.Fatalf("blank: got %v %q %v", amt, cur, err)
	}
	if _, _, err := ParseOriginal("lots USD"); err == nil {
		t.Fatal("expected error")
	}
}

func TestPreviewIncludesExpensesAndCommitMarksThemBilled(t *testing.T) {
	dataDir, man := setupProject(t,
		`aaa,"Task one",2026-09-01T09:00:00Z,2026-09-01T11:00:00Z,true`, // 2h @ 100
	)
	e := mustAddExpense(t, dataDir, Expense{Date: "2026-09-15", Description: "OpenRouter", Amount: 42.5})

	inv, err := Preview(dataDir, man, Options{ProjectSlug: "acme-website"})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(inv.Expenses) != 1 || inv.Expenses[0].UUID != e.UUID {
		t.Fatalf("expected the expense in preview, got %+v", inv.Expenses)
	}
	if inv.TimeSubtotal != 200 || inv.ExpensesSubtotal != 42.5 || inv.TotalAmount != 242.5 {
		t.Fatalf("totals wrong: time=%v exp=%v total=%v", inv.TimeSubtotal, inv.ExpensesSubtotal, inv.TotalAmount)
	}

	committed, err := Commit(dataDir, man, Options{ProjectSlug: "acme-website"})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	loaded, err := LoadLedger(ledgerPath(dataDir, committed.Number))
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Expenses) != 1 || loaded.Expenses[0].Description != "OpenRouter" {
		t.Fatalf("ledger should snapshot the expense, got %+v", loaded.Expenses)
	}

	unbilled, err := UnbilledExpenses(dataDir, "acme-website")
	if err != nil {
		t.Fatal(err)
	}
	if len(unbilled) != 0 {
		t.Fatalf("expense should be billed after commit, still unbilled: %+v", unbilled)
	}
	again, err := Preview(dataDir, man, Options{ProjectSlug: "acme-website"})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Expenses) != 0 {
		t.Fatal("billed expense must never be billed twice")
	}
}

func TestExpenseOnlyInvoiceCommits(t *testing.T) {
	dataDir, man := setupProject(t)
	mustAddExpense(t, dataDir, Expense{Date: "2026-09-15", Description: "OpenRouter", Amount: 10})

	inv, err := Commit(dataDir, man, Options{ProjectSlug: "acme-website"})
	if err != nil {
		t.Fatalf("expense-only invoice should commit: %v", err)
	}
	if len(inv.LineItems) != 0 || inv.TotalAmount != 10 {
		t.Fatalf("unexpected invoice: %+v", inv)
	}
	if _, err := Commit(dataDir, man, Options{ProjectSlug: "acme-website"}); err == nil {
		t.Fatal("second commit with nothing unbilled should be refused")
	}
}

func TestExpensesRespectDateRange(t *testing.T) {
	dataDir, man := setupProject(t)
	mustAddExpense(t, dataDir, Expense{Date: "2026-08-31", Description: "before", Amount: 1})
	mustAddExpense(t, dataDir, Expense{Date: "2026-09-01", Description: "first day", Amount: 2})
	mustAddExpense(t, dataDir, Expense{Date: "2026-09-30", Description: "last day", Amount: 3})
	mustAddExpense(t, dataDir, Expense{Date: "2026-10-01", Description: "after", Amount: 4})

	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	to := time.Date(2026, 9, 30, 0, 0, 0, 0, time.Local).Add(24*time.Hour - time.Nanosecond)
	inv, err := Preview(dataDir, man, Options{ProjectSlug: "acme-website", From: &from, To: &to})
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Expenses) != 2 || inv.ExpensesSubtotal != 5 {
		t.Fatalf("expected only the two September expenses, got %+v", inv.Expenses)
	}
}

func TestExpenseVAT(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "focuson-data")
	if err := manifest.Bootstrap(dataDir); err != nil {
		t.Fatal(err)
	}
	// rate_includes_vat must not affect expenses: they're always ex-VAT.
	if _, err := manifest.AddClient(dataDir, manifest.Client{Name: "Local Co", Currency: "ZAR", Rate: 115, VATPercent: 15, RateIncludesVAT: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.AddProject(dataDir, manifest.Project{Name: "Acme Website", Client: "local-co"}); err != nil {
		t.Fatal(err)
	}
	man, err := manifest.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dataDir, "projects", "acme-website", "task_log.csv")
	if err := os.WriteFile(logPath, []byte("uuid,task,from,to,completed\naaa,\"T\",2026-09-01T09:00:00Z,2026-09-01T10:00:00Z,true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustAddExpense(t, dataDir, Expense{Date: "2026-09-02", Description: "recharge", Amount: 100})
	mustAddExpense(t, dataDir, Expense{Date: "2026-09-03", Description: "disbursement", Amount: 50, NoVAT: true})

	inv, err := Preview(dataDir, man, Options{ProjectSlug: "acme-website"})
	if err != nil {
		t.Fatal(err)
	}
	// time: 115 incl VAT -> 100 + 15. expenses: 150 ex VAT, VAT only on 100 -> 15.
	if inv.TimeSubtotal != 100 || inv.ExpensesSubtotal != 150 {
		t.Fatalf("subtotals: time=%v exp=%v", inv.TimeSubtotal, inv.ExpensesSubtotal)
	}
	if inv.Subtotal != 250 || inv.VATAmount != 30 || inv.TotalAmount != 280 {
		t.Fatalf("totals: sub=%v vat=%v total=%v", inv.Subtotal, inv.VATAmount, inv.TotalAmount)
	}
}

func TestReconFlagsStaleExpense(t *testing.T) {
	dataDir, man := setupProject(t)
	old := time.Now().AddDate(0, 0, -30).Format(ExpenseDateFormat)
	recent := time.Now().Format(ExpenseDateFormat)
	stale := mustAddExpense(t, dataDir, Expense{Date: old, Description: "old", Amount: 1})
	mustAddExpense(t, dataDir, Expense{Date: recent, Description: "new", Amount: 1})

	issues, err := Recon(dataDir, man, 14*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].UUID != stale.UUID || issues[0].Kind != "stale" {
		t.Fatalf("expected one stale expense issue, got %+v", issues)
	}
}
