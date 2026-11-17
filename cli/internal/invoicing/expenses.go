package invoicing

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ckritzinger/focus_on/cli/internal/tasklog"
)

// ExpenseDateFormat is how expense dates are stored, both in expense_log.csv
// and in the invoice ledger: a plain calendar date, no time or zone — a
// receipt is dated, not timestamped.
const ExpenseDateFormat = "2006-01-02"

var expenseLogHeader = []string{"uuid", "date", "description", "amount", "original_amount", "original_currency", "no_vat", "receipt"}

// Expense is one row of a project's expense_log.csv: a cost incurred on the
// client's behalf, to be reimbursed on the next invoice (spec_v2.md,
// "expense_log.csv"). It doubles as the frozen [[expense]] snapshot in an
// invoice ledger file.
//
// Amount is always in the client's currency and always ex-VAT; VAT is added
// on top at the client's rate unless NoVAT is set (a pure disbursement).
// OriginalAmount/OriginalCurrency are reference-only (e.g. the USD figure on
// an OpenRouter receipt) — printed on the invoice, never converted.
type Expense struct {
	UUID             string  `toml:"uuid"`
	Date             string  `toml:"date"` // ExpenseDateFormat
	Description      string  `toml:"description"`
	Amount           float64 `toml:"amount"`
	OriginalAmount   float64 `toml:"original_amount,omitempty"`
	OriginalCurrency string  `toml:"original_currency,omitempty"`
	NoVAT            bool    `toml:"no_vat,omitempty"`
	Receipt          string  `toml:"receipt,omitempty"` // path, reference only
}

// OriginalLabel is e.g. "67.89 USD", or "" when no original amount was given.
func (e Expense) OriginalLabel() string {
	if e.OriginalAmount <= 0 {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%.2f %s", e.OriginalAmount, e.OriginalCurrency))
}

// Summary is a one-line terminal rendering: date, amount, description,
// plus the original amount and a no-VAT marker when set.
func (e Expense) Summary() string {
	line := fmt.Sprintf("%s  %10.2f  %s", e.Date, e.Amount, e.Description)
	if orig := e.OriginalLabel(); orig != "" {
		line += " (" + orig + ")"
	}
	if e.NoVAT {
		line += " [no VAT]"
	}
	return line
}

func expenseLogPath(dataDir, projectSlug string) string {
	return filepath.Join(dataDir, "projects", projectSlug, "expense_log.csv")
}

// ReadExpenses reads a project's expense_log.csv. A missing file just means
// no expenses have ever been recorded for that project.
func ReadExpenses(dataDir, projectSlug string) ([]Expense, error) {
	path := expenseLogPath(dataDir, projectSlug)
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = len(expenseLogHeader)
	if _, err := r.Read(); err != nil { // header
		if err == io.EOF {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s header: %w", path, err)
	}

	var out []Expense
	for {
		fields, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		line, _ := r.FieldPos(0)
		e, err := parseExpense(fields)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		out = append(out, e)
	}
	return out, nil
}

func parseExpense(fields []string) (Expense, error) {
	e := Expense{
		UUID:             strings.TrimSpace(fields[0]),
		Date:             strings.TrimSpace(fields[1]),
		Description:      fields[2],
		OriginalCurrency: strings.TrimSpace(fields[5]),
		Receipt:          fields[7],
	}
	if e.UUID == "" {
		return Expense{}, fmt.Errorf("missing uuid")
	}
	if _, err := time.Parse(ExpenseDateFormat, e.Date); err != nil {
		return Expense{}, fmt.Errorf("invalid date %q (want YYYY-MM-DD)", e.Date)
	}
	amount, err := strconv.ParseFloat(strings.TrimSpace(fields[3]), 64)
	if err != nil {
		return Expense{}, fmt.Errorf("invalid amount %q", fields[3])
	}
	e.Amount = amount
	if s := strings.TrimSpace(fields[4]); s != "" {
		orig, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return Expense{}, fmt.Errorf("invalid original_amount %q", s)
		}
		e.OriginalAmount = orig
	}
	switch strings.ToLower(strings.TrimSpace(fields[6])) {
	case "", "false":
	case "true":
		e.NoVAT = true
	default:
		return Expense{}, fmt.Errorf("invalid no_vat %q (want true/false)", fields[6])
	}
	return e, nil
}

// ValidateExpense checks the fields a human enters. Amounts must be
// positive — credits/refunds are out of scope (handle by hand).
func ValidateExpense(e Expense) error {
	if strings.TrimSpace(e.Description) == "" {
		return fmt.Errorf("description is required")
	}
	if _, err := time.Parse(ExpenseDateFormat, e.Date); err != nil {
		return fmt.Errorf("date %q: expected YYYY-MM-DD", e.Date)
	}
	if e.Amount <= 0 {
		return fmt.Errorf("amount must be greater than zero")
	}
	if e.OriginalAmount < 0 {
		return fmt.Errorf("original amount can't be negative")
	}
	if e.OriginalCurrency != "" && e.OriginalAmount == 0 {
		return fmt.Errorf("original currency given without an original amount")
	}
	return nil
}

// ParseOriginal parses "67.89 USD" (or "67.89") into amount and currency.
// Blank returns zeros.
func ParseOriginal(s string) (float64, string, error) {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return 0, "", nil
	}
	if len(parts) > 2 {
		return 0, "", fmt.Errorf("expected e.g. \"67.89 USD\", got %q", s)
	}
	amount, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, "", fmt.Errorf("expected e.g. \"67.89 USD\", got %q", s)
	}
	cur := ""
	if len(parts) == 2 {
		cur = strings.ToUpper(parts[1])
	}
	return amount, cur, nil
}

// AddExpense validates e, assigns it a fresh UUID, and appends it to the
// project's expense_log.csv. Append-only, like task_log.csv: fixing a typo
// in an unbilled expense means editing the file by hand.
func AddExpense(dataDir, projectSlug string, e Expense) (Expense, error) {
	e.Amount = roundMoney(e.Amount)
	if err := ValidateExpense(e); err != nil {
		return Expense{}, err
	}
	projectDir := filepath.Join(dataDir, "projects", projectSlug)
	if info, err := os.Stat(projectDir); err != nil || !info.IsDir() {
		return Expense{}, fmt.Errorf("no projects/%s directory — add the project first", projectSlug)
	}
	e.UUID = tasklog.NewUUID()

	path := expenseLogPath(dataDir, projectSlug)
	isNew := false
	if info, err := os.Stat(path); os.IsNotExist(err) || (err == nil && info.Size() == 0) {
		isNew = true
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return Expense{}, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if isNew {
		if err := w.Write(expenseLogHeader); err != nil {
			return Expense{}, err
		}
	}
	orig := ""
	if e.OriginalAmount > 0 {
		orig = strconv.FormatFloat(e.OriginalAmount, 'f', -1, 64)
	}
	if err := w.Write([]string{
		e.UUID,
		e.Date,
		e.Description,
		strconv.FormatFloat(e.Amount, 'f', 2, 64),
		orig,
		e.OriginalCurrency,
		strconv.FormatBool(e.NoVAT),
		e.Receipt,
	}); err != nil {
		return Expense{}, err
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return Expense{}, fmt.Errorf("writing %s: %w", path, err)
	}
	return e, nil
}

// UnbilledExpenses returns a project's expenses that no invoice has claimed
// yet, oldest first (file order).
func UnbilledExpenses(dataDir, projectSlug string) ([]Expense, error) {
	all, err := ReadExpenses(dataDir, projectSlug)
	if err != nil {
		return nil, err
	}
	billed, err := billedUUIDs(dataDir, projectSlug)
	if err != nil {
		return nil, err
	}
	var out []Expense
	for _, e := range all {
		if !billed[e.UUID] {
			out = append(out, e)
		}
	}
	return out, nil
}

// expenseInRange reports whether an expense's date falls within the
// optional From/To bounds. The date is taken as local midnight, matching how
// --from/--to are parsed (start/end of a local day).
func expenseInRange(e Expense, from, to *time.Time) bool {
	d, err := time.ParseInLocation(ExpenseDateFormat, e.Date, time.Local)
	if err != nil {
		return false
	}
	if from != nil && d.Before(*from) {
		return false
	}
	if to != nil && d.After(*to) {
		return false
	}
	return true
}
