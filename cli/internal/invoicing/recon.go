package invoicing

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/ckritzinger/focus_on/cli/internal/manifest"
	"github.com/ckritzinger/focus_on/cli/internal/tasklog"
)

// ReconIssue is one thing worth a human's attention, surfaced by Recon.
type ReconIssue struct {
	Kind    string // "duplicate" or "stale"
	Project string
	UUID    string
	Task    string
	Detail  string
}

// Recon runs two cheap sanity checks across every billable project (see
// spec_v2.md, "Double-billing prevention"):
//
//   - duplicate: the same task_log.csv or expense_log.csv UUID billed by
//     more than one invoice. Should be structurally impossible given how
//     Commit works, but it's a one-line query and free insurance against a
//     hand-edited invoiced.csv.
//   - stale: a closed, billable-length row (or an expense_log.csv expense)
//     older than staleAfter with no invoiced.csv entry at all — the omission
//     failure mode a pure timestamp-cursor design would be prone to (see the
//     "backdated entry" discussion in spec_v2.md).
func Recon(dataDir string, man manifest.Manifest, staleAfter time.Duration) ([]ReconIssue, error) {
	var issues []ReconIssue
	cutoff := time.Now().Add(-staleAfter)

	for _, p := range man.Projects {
		if !p.Billable() {
			continue
		}

		invoicedRows, err := readInvoiced(invoicedPath(dataDir, p.Slug))
		if err != nil {
			return nil, err
		}
		seenBy := make(map[string][]string) // uuid -> invoice numbers that claim it
		for _, r := range invoicedRows {
			seenBy[r.UUID] = append(seenBy[r.UUID], r.InvoiceNumber)
		}
		for uuid, invoiceNumbers := range seenBy {
			if len(invoiceNumbers) > 1 {
				issues = append(issues, ReconIssue{
					Kind:    "duplicate",
					Project: p.Slug,
					UUID:    uuid,
					Detail:  fmt.Sprintf("billed by %v", invoiceNumbers),
				})
			}
		}

		taskLogPath := filepath.Join(dataDir, "projects", p.Slug, "task_log.csv")
		rows, err := tasklog.ReadRows(taskLogPath)
		if err != nil {
			return nil, err
		}
		billed := make(map[string]bool, len(invoicedRows))
		for _, r := range invoicedRows {
			billed[r.UUID] = true
		}
		for _, r := range rows {
			if r.To == nil || r.To.Sub(r.From) < time.Minute {
				continue // not billable in the first place — see Preview
			}
			if billed[r.UUID] {
				continue
			}
			if r.To.After(cutoff) {
				continue // recent enough that you probably just haven't invoiced yet
			}
			issues = append(issues, ReconIssue{
				Kind:    "stale",
				Project: p.Slug,
				UUID:    r.UUID,
				Task:    r.Task,
				Detail:  fmt.Sprintf("closed %s, never invoiced", r.To.Format("2006-01-02")),
			})
		}

		expenses, err := ReadExpenses(dataDir, p.Slug)
		if err != nil {
			return nil, err
		}
		for _, e := range expenses {
			if billed[e.UUID] {
				continue
			}
			d, err := time.ParseInLocation(ExpenseDateFormat, e.Date, time.Local)
			if err != nil || d.After(cutoff) {
				continue
			}
			issues = append(issues, ReconIssue{
				Kind:    "stale",
				Project: p.Slug,
				UUID:    e.UUID,
				Task:    "expense: " + e.Description,
				Detail:  fmt.Sprintf("dated %s, never invoiced", e.Date),
			})
		}
	}
	return issues, nil
}
