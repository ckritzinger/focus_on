# FocusOn v2 — Harvest Replacement Spec

## Overview

v1 (`spec.md`) is a single macOS widget that logs task sessions to one flat CSV. v2 turns FocusOn into a small personal invoicing system: multiple clients, multiple projects, per-project time logs, and PDF invoice generation — without becoming Harvest-shaped (accounts, subscriptions, cloud sync, a web app).

This is additive to v1. Everything in `spec.md` about the floating widget's window behaviour, dragging, popover mechanics, and menu bar item is unchanged unless called out below.

### Goals

- Replace Harvest for one freelancer (Carl), tracking time against clients/projects and generating invoice PDFs.
- **Strict split**: the widget is *only* about logging time — live tracking plus backdated manual entries, nothing else. The CLI is *only* about client/project config and turning logged time into invoices. The widget never knows about rates, clients, or invoices. The one deliberate exception: `focuson log` (see "CLI manual entry" below) also writes `task_log.csv`, as a flake-resistant alternative to the widget's own "Log past session" form, in the same format and under the same open-session invariant.
- Keep everything as plain text files in a git repo — diffable, greppable, no server, no database file to corrupt.

### Non-goals

- No multi-user support, no cloud sync, no mobile app.
- No tax/VAT calculation or multi-currency conversion — currency is recorded per client for display only.
- No editing of past task sessions from the UI (still true from v1) — corrections happen via the widget's "Log past session" action, which appends, never rewrites.
- No automatic git commits from the widget or the CLI's normal flow — sync is an explicit step.

---

## System Architecture

Two components:

1. **FocusOn.app** (Swift, unchanged tech stack from v1) — the floating widget + menu bar tracker. Only job: log time. Gains a project picker, a "Log past session" manual-entry action, and awareness of a *data directory* instead of a single log file. Never touches `manifest.toml`, rates, or invoices.
2. **`focuson` CLI** (Go, using Bubble Tea for any interactive views) — new. Only job: client/project config and turning logged time into invoices. Owns client/project management, invoice generation, PDF rendering, and git sync. Reads `task_log.csv`/`invoiced.csv` with a plain `encoding/csv`-based reader (`internal/tasklog`) and filters/joins in Go — no DuckDB, no separate database file. (DuckDB was the original plan for this, but by the time invoice generation was built, `internal/tasklog` already existed for the startup integrity check and fully covers what's needed — filter + sum over a few hundred rows doesn't need a query engine, and skipping it avoids a CGO dependency for zero benefit at this data scale. It's still a reasonable option later for a cross-project glob report like `focuson status`, if one gets built.) Otherwise never writes `task_log.csv` — `focuson log` (flags or TUI "Log Time") is the sole exception, see "CLI manual entry" below.

Both components read/write the same **data directory**, which is its own git repository, separate from the FocusOn app source repo.

---

## Data Directory Layout

```
~/focuson-data/                        (a git repo)
├── manifest.toml                      # business info, clients, projects, rates
├── projects/
│   ├── personal/
│   │   ├── task_log.csv
│   │   └── invoiced.csv
│   ├── acme-website/
│   │   ├── task_log.csv
│   │   └── invoiced.csv
│   └── <project-slug>/
│       ├── task_log.csv
│       └── invoiced.csv
└── invoices/
    ├── INV-0041.toml                  # placeholder baseline (see Invoice Numbering)
    ├── INV-0042.toml                  # generated invoice + its PDF
    └── INV-0042.pdf
```

- The directory location is user-chosen (see Widget Changes) and stored once; nothing hardcodes `~/focuson-data`.
- Each project gets its own subdirectory under `projects/`, named by its slug. The log file inside is **always** named `task_log.csv` — no per-project filename configuration. This leaves room to put other project-scoped files (e.g. exported reports) alongside it later without a renaming scheme.
- `manifest.toml` and `invoices/*.toml` are the only files the CLI writes to directly, plus `task_log.csv` via `focuson log` (see "CLI manual entry" below). The widget only ever appends to a `task_log.csv` and only ever reads `manifest.toml` (never writes it).

---

## CSV Schema (`task_log.csv`, per project)

```
uuid,task,from,to,completed
```

| Column | Type | Notes |
|---|---|---|
| `uuid` | String | UUIDv4, assigned once per session at start. See "Session pairing" below. |
| `task` | String | Free text, same quoting rules as v1. |
| `from` | ISO 8601 datetime | Same as v1. |
| `to` | ISO 8601 datetime | Empty if session still open. |
| `completed` | Boolean string | Same as v1 — `true`/`false`/empty. |

No migration from v1's 4-column `task_log.csv` — this is a fresh start (per prior decision). Old data isn't imported.

### Session pairing via UUID

The widget's write model is unchanged from v1: switching tasks appends a **closing row** for the old task and an **opening row** for the new task as two separate lines (append-only — the opening row is never rewritten in place). To let both lines refer to the same session:

- When a task is started, generate a new UUID and persist it (`TaskStore.currentTaskUUID`, alongside `currentTaskName`/`currentTaskStartedAt` in `UserDefaults`).
- The **opening row** (`to` and `completed` empty) is written with that UUID.
- Whenever that session later closes — via switching tasks, "Complete task", "Pause", or the terminate-time closing write — the **closing row** is written reusing the *same* stored UUID, then the stored UUID is cleared.

Net effect: a finished session appears as two CSV rows sharing one UUID — one with `to` empty (the open marker) and one with `to` filled in (the close marker, carrying the real duration and completion status).

**Consumers (invoice generation) only care about rows where `to IS NOT NULL`.** Each UUID has at most one such row, so filtering on `to IS NOT NULL` yields exactly one billable row per finished session — no further de-duplication needed. A UUID that appears only as an open row (no matching `to`-filled row) is either the currently active task or an orphaned session from a crash (same crash caveat as v1) — either way, correctly excluded from billing since its duration is unknown.

### Manual / post-hoc entries

For time you forgot to track live: a **widget** action (see Widget Changes → "Log past session") writes a **single row** directly with `uuid`, `task`, `from`, `to`, and `completed` all set at once — there's no "open" phase to pair, so no second row is written. This lives in the widget, not the CLI: it's just another append to `task_log.csv`, no billing logic involved, and the widget already owns the project list, `CSVLogger`, and UUID generation needed to do it. Historically the CLI never wrote `task_log.csv` at all — only the widget did (live tracking and manual entry alike), with the CLI writing only `invoiced.csv` and `invoices/*.toml`. `focuson log` (see "CLI manual entry" further down) is now a second, deliberate manual-entry path with the same row shape, added because the widget's form above turned out to be flaky in practice.

### Startup data-integrity check

The CLI trusts a `to`-less row to mean exactly one thing: "still being actively tracked right now." That's only true for the widget's live in-memory state — the CLI has no way to ask the widget "is this the session currently running?" — so it uses position as a proxy: **the last row of a project's `task_log.csv` is given the benefit of the doubt; any earlier row with no `to` is presumed abandoned** (a crash-orphaned row, v1's documented limitation, that got buried under later, unrelated sessions instead of being the most recent thing logged).

On every startup, `focuson` scans every project's `task_log.csv` for exactly this — a UUID with no `to`-filled row anywhere, whose row isn't the file's last line — before it will do anything else, and refuses to start if it finds one:

```
focuson: refusing to start — 1 dangling task_log.csv entry found (started but never closed, and not the most recent thing logged):

  projects/powersync/task_log.csv:14  64c65f27-...  "Building out prompt for uploadData generation"  started 2026-09-06T09:14:23Z, never closed

Edit the file by hand to fix it — give it a `to` and `completed` value if the
time really was worked, or delete the row if it wasn't — then run focuson again.
```

A row with the wrong number of fields or an unparseable timestamp is treated the same way (refuse to start) — a checker that silently skipped rows it couldn't parse would defeat its own purpose. A normal open/close pair (same UUID, one row with `to` empty and one with it filled) is never flagged regardless of where it sits in the file — only a UUID that never gets closed anywhere is a problem. There is no auto-fix: this is a manual, by-hand edit to the CSV, on purpose — it's real billable-or-not-billable time on the line, not something to guess at programmatically.

Implemented in `internal/tasklog` (`ReadRows`, `CheckDataDirectory`), called from `main.go` before the TUI starts.

---

## `invoiced.csv` (per project, alongside `task_log.csv`)

```
uuid,invoice_number,invoiced_at
```

An append-only map from task-log UUID to the invoice that billed it. Written **only** by the CLI, at the moment an invoice is committed (never by the widget, never edited or deleted). This is the actual mechanism that prevents double-billing — see below.

| Column | Type | Notes |
|---|---|---|
| `uuid` | String | Matches a `task_log.csv` row's `uuid`. |
| `invoice_number` | String | e.g. `INV-0042`. |
| `invoiced_at` | ISO 8601 datetime | When the invoice was generated. |

---

## `manifest.toml` Schema

```toml
[business]
name = "Carl Kritzinger"
address = "..."
email = "ckritzinger@gmail.com"
registration_number = "..."      # optional: company reg number, printed under the address
vat_note = "..."                 # optional free text, e.g. "Not registered for VAT" or "VAT No: ..." — no jurisdiction assumed
payment_terms = "..."            # optional free text, e.g. "Due upon receipt" or "Net 30" — printed as-is, never computed into a date
payment_details = "..."          # free text: bank details / payment instructions, printed on invoices

[[client]]
slug = "acme"
name = "Acme Corp"
currency = "USD"
rate = 120.0                     # default hourly rate for this client's projects
address = "..."                  # billing address, printed on invoice
contact_email = "..."

[[project]]
slug = "acme-website"
client = "acme"                  # references client.slug
name = "Acme Website Redesign"
rate = 140.0                     # optional override of client.rate
last_invoiced_at = "2026-08-01T00:00:00Z"   # cursor — perf optimization only, not the billing source of truth

[[project]]
slug = "personal"
client = ""                      # blank/absent client = non-billable project
name = "Personal"
```

- Every project needs a slug (used as its directory name under `projects/`). `client` blank means the project never shows up as invoiceable.
- On first use of a data directory, if `manifest.toml` doesn't exist yet, it's created with just the `personal` project (no `[business]`/clients yet — CLI prompts for those on first `client add`/invoice attempt). If it already exists, it's used as-is — selecting a directory never overwrites existing manifest data.

---

## Invoice Ledger (`invoices/INV-000N.toml`)

One file per invoice, never a single growing ledger file — keeps git diffs to "one new file per invoice" and makes a botched invoice a single file to delete.

This TOML file is a **frozen historical snapshot** for humans (and PDF regeneration) — it's not consulted for double-billing logic (that's `invoiced.csv`, above). Keeping full `line_item`s here means an old invoice's exact content is never dependent on reinterpreting `task_log.csv` later.

```toml
number = "INV-0042"
project = "acme-website"
client = "acme"
generated_at = "2026-09-06T10:00:00Z"
period_from = "2026-08-01T00:00:00Z"
period_to = "2026-09-06T09:00:00Z"
currency = "USD"
rate = 140.0
total_hours = 32.5
total_amount = 4550.00
pdf_path = "invoices/INV-0042.pdf"
placeholder = false

[[line_item]]
uuid = "b3f1c2a0-..."
task = "Homepage redesign"
from = "2026-08-03T09:00:00Z"
to = "2026-08-03T11:30:00Z"
hours = 2.5
```

### Invoice numbering & baseline

To keep numbering consistent with the existing Harvest sequence, the counter isn't reset to 1 — it continues from wherever Harvest left off. This is done by creating a **placeholder invoice** for the last number Harvest issued:

```toml
# invoices/INV-0041.toml
number = "INV-0041"
placeholder = true
note = "Baseline import — last invoice number issued by Harvest before migrating to FocusOn."
generated_at = "2026-09-06T09:00:00Z"
```

Placeholders have no `project`/`client`/`line_item`s. The next-invoice-number calculation is always `max(existing invoice numbers across invoices/*.toml, placeholder or not) + 1`, so creating `INV-0041` as a placeholder makes the next real invoice `INV-0042`. Created via `focuson invoice set-last --number 41`.

### Double-billing prevention

`task_log.csv` stays fully immutable — never rewritten by the widget *or* the CLI, no `invoice_id` column bolted onto it. Instead, billing state lives in the separate append-only `invoiced.csv` map file. This makes exclusion a native SQL anti-join, no Go-side set-building or TOML-parsing required:

```sql
SELECT t.*
FROM read_csv_auto('projects/<slug>/task_log.csv') t
LEFT JOIN read_csv_auto('projects/<slug>/invoiced.csv') i USING (uuid)
WHERE t.to IS NOT NULL
  AND i.uuid IS NULL
  -- optional: AND t.to > ? / AND t.to < ? for --from/--to
```

- `last_invoiced_at` on the project manifest entry is purely a scan-skip optimization (a lower bound a future large-scale implementation could use to skip old rows) — the anti-join against `invoiced.csv` is what's actually correct regardless of the cursor's value, so a wrong/stale cursor can only cost a slower scan, never a wrong result. In practice `internal/invoicing.Preview` doesn't even use it yet — it scans the whole file every time, which is fine at the row counts a solo freelancer's log actually reaches.
- Because exclusion is keyed on `invoiced.csv`, not on timestamps, a backdated manual entry (added via the widget's "Log past session," the whole point of that feature) is picked up correctly on the next `invoice generate` no matter how far in the past its `to` falls — it simply isn't in `invoiced.csv` yet.
- **Recon is now a one-line SQL sanity check**, cheap enough to run after every generation or on demand: `SELECT uuid, COUNT(*) FROM read_csv_auto('projects/*/invoiced.csv') GROUP BY uuid HAVING COUNT(*) > 1` — any result means the same task-log row got billed twice (should be structurally impossible given the anti-join, but cheap insurance against hand-edited files). A second, equally cheap check catches *omission* (the bug a pure-cursor approach is prone to): closed rows older than N days with no matching `invoiced.csv` entry — the same anti-join query above, just with an age filter instead of the manifest cursor.

---

## Invoice Generation Flow

TUI: Invoices → generate new. Flag equivalent: `focuson invoice generate --project <slug> [--from <iso>] [--to <iso>] [--dry-run]`

1. Resolve the project (and its client, rate, currency) from `manifest.toml`.
2. Run the anti-join from "Double-billing prevention" against `task_log.csv` + `invoiced.csv` (`internal/invoicing.Preview`), optionally bounded by `--from`/`--to` (default: no explicit bound — the anti-join against `invoiced.csv` already excludes everything previously billed). Result comes back as plain Go structs — nothing further to exclude.
3. Compute `hours = to - from` per row, `total_hours`, `total_amount = total_hours * rate`.
4. Render line items **one per raw task entry** (no daily rollup — matches actual usage, not a lot of task-switching).
5. **Dry-run**: print the would-be line items + totals to stdout. No files written, no counter bumped, no `invoiced.csv` rows written.
6. **Real run**: write `invoices/INV-000N.toml` (full line-item snapshot — see below), render `invoices/INV-000N.pdf` (pure-Go PDF lib — `gofpdf` or `maroto`, no external binary dependency), append one row per billed UUID to `invoiced.csv` (`uuid,INV-000N,now`), bump `last_invoiced_at` on the project.
7. Nothing is committed to git automatically — run `focuson sync` afterward.

One invoice always covers exactly one project. A client with multiple projects gets one invoice per project, run separately.

Invoice PDF contents: `[business]` header (name/address/email/registration number/VAT note/payment terms), client billing info, invoice number + date + period, line-item table (task, from, to, hours), total hours and amount, payment details footer. Exact visual layout is an implementation detail, not specified further here.

---

## Interface: Bubble Tea TUI (primary), flags (secondary)

Running `focuson` with no arguments launches an interactive Bubble Tea TUI — this is the intended day-to-day interface. Nothing about this tool should require memorizing flags; every action below is reachable from an on-screen menu.

Note: logging time (live or backdated) isn't here at all — that's entirely the widget's job (see Widget Changes → "Log past session"). This TUI only covers the billing side: clients, projects, invoices, sync.

**Main menu:**

```
FocusOn

  ▸ Business
    Clients
    Projects
    Invoices
    Sync
    Quit
```

- **Business** — a singleton, so straight into an edit form (no list): name, address, email, registration number, VAT note, payment terms, payment details. Address and payment details are entered as one `"; "`-separated line (same convention as a client's address) since the form's fields are single-line — `pdfgen` splits them back into separate printed lines.
- **Clients** — list existing clients (name, currency, rate); "add new" opens a form (name, currency, rate, address, contact email); select one to edit.
- **Projects** — list existing projects (name, client, effective rate); "add new" opens a form (name, slug, client picker, rate override); select one to edit.
- **Invoices** — list existing invoices (number, project, period, total) newest first; "generate new" walks: pick project → optional date range (blank = everything unbilled) → shows computed line items + total for review *before* anything is written (this review step **is** the dry-run — no separate mode to remember) → confirm to commit (writes ledger + PDF, appends to `invoiced.csv`, bumps counter) or back out with nothing touched; "set starting number" prompts for N and a note, writes the placeholder invoice (Harvest continuity); "check consistency" runs the dup/staleness recon queries and shows any hits.
- **Sync** — shows `git status`-style summary of what changed in the data directory, confirm to stage and commit, then push to `origin` if that remote is configured (pushes even on a clean tree, in case an earlier commit is sitting unpushed — see Phase 9). Push failure is never fatal — a local commit is real durability on its own.

Each screen has its own escape/back key to return to the main menu without side effects.

### Flags, for scripting only

The same actions are available non-interactively for automation (e.g. a cron job that reminds you of unbilled hours), but this is a secondary surface — nothing here needs to be memorized for normal use:

| Command | Purpose |
|---|---|
| `focuson invoice generate --project <slug> [--from] [--to] [--dry-run]` | Same as "generate new" (`--dry-run` = review-only, matching the TUI's review step). |
| `focuson invoice set-last --number <N> [--note "..."]` | Same as "set starting number". |
| `focuson sync` | Same as the "Sync" screen, no confirmation prompt (assumes non-interactive use). |

`focuson status` (unbilled hours summary per project, e.g. for a cron reminder) is a plausible future addition, reachable either way, but not required for v1 of this spec.

---

## Widget Changes (FocusOn.app)

### Data directory instead of a log file

v1 let you pick an arbitrary CSV file path (`CSVLogger.setFilePath`, exposed as "Change…" in the popover, stored in `UserDefaults` as `focuson.logFilePath`). This is replaced:

- New setting `focuson.dataDirectoryPath` (`UserDefaults`), chosen via an `NSOpenPanel` with `canChooseDirectories = true`, `canChooseFiles = false`, `canCreateDirectories = true`.
- On selecting a directory: if `manifest.toml` doesn't exist there, create a minimal one (just the `personal` project, no business/client info yet — that's the CLI's job) **and** create `projects/personal/` so the picker isn't empty on first run. If a manifest already exists, use the directory untouched.
- `CSVLogger`'s file path becomes a function of `(dataDirectory, projectSlug) → <dataDirectory>/projects/<projectSlug>/task_log.csv`, creating the project subdirectory and CSV header (`uuid,task,from,to,completed`) on first write if missing.
- The popover's "Log file" display becomes "Data directory", showing the resolved directory path.

### Project picker

- The task-selection UI (`TaskSelectionView`) gains a project picker, populated by **listing subdirectories of `projects/`** — not by parsing `manifest.toml` at all. Each subdirectory name *is* the project slug, used directly as the display label (e.g. `acme-website`). Defaults to `personal`, or the last-used project (persisted in `UserDefaults`).
- This means the widget needs **zero TOML parsing** — no dependency, no hand-rolled parser, nothing to keep in sync with the manifest schema. The directory listing is the single source of truth for "which projects exist," which the CLI already needs to keep consistent with `manifest.toml` anyway.
- Consequence: `focuson project add` must create `projects/<slug>/` (with an empty `task_log.csv` header) immediately, not lazily on first log write — otherwise a newly-added project wouldn't appear in the widget's picker until first touched from the CLI.
- Creating new clients/projects is **CLI-only** — the widget only ever selects from the existing list of directories.
- `TaskStore` gains `currentProjectSlug: String?`, persisted like the other current-task state, so the right file gets the closing row on task switch/complete/pause/terminate.

### CSV write changes

- `CSVLogger.appendRow` gains a `uuid` parameter, written as the first column.
- `TaskStore.startTask` generates a new UUID when opening the new session and stores it (`currentTaskUUID`); the closing append (for the previous task) reuses whatever UUID was stored for that prior session.
- `completeCurrentTask`, `pauseCurrentTask`, and `writeClosingRowOnTerminate` all use the stored `currentTaskUUID` for their closing row, then clear it.

### Log past session (manual / post-hoc entry)

**v2.1 revision**: this used to be a separate action in the widget's action popover ("🕒 Log past session", its own sheet) — removed because that popover is an `NSPopover` on a `.nonactivatingPanel`, and its `TextField` reliably failed to take keyboard focus (typed input fell through to whatever app was last active). `TaskSelectionView` had already hit and worked around the same focus bug for its own text field (see its `onAppear`'s deferred `@FocusState` assignment); `LogPastSessionView` never got that fix, and rather than patch it in twice, the whole flaky form was folded into `TaskSelectionView` instead of being fixed in place:

- The "New task…" row (task-selection UI, same screen used to start a task normally) gained an always-visible From/To date-and-time picker row directly below it, plus a "Done" completed-checkbox that only appears once those pickers have been touched.
- Both default to "now" on open, which looks identical to "untouched" — `TaskSelectionView` tracks a separate `hasEditedTimes` flag, set the instant either picker's value actually changes, to distinguish the two.
- `hasEditedTimes == false` at submit time → the normal live-start path (`onSelect` → `TaskStore.startTask`), exactly as before this change.
- `hasEditedTimes == true` → a new `onLogPast` callback (`TaskStore.logPastSession`) instead: validates `to > from`, generates a fresh UUID, and calls `CSVLogger.appendRow` **once**, directly, with all fields set. No pairing, no interaction with `currentTaskName`/`currentTaskStartedAt`/`currentTaskUUID` — a standalone append to whichever project's `task_log.csv` was picked, independent of whatever task (if any) is currently active. The button label switches from "Start" to "Log" once `hasEditedTimes` is true, as the only other visible cue.
- This only applies to the new-task text field, not to picking an existing recent task from the list above it — recent-task rows still single-click straight into a live start, unchanged.
- Known consequence of removing the standalone popover action: there's no longer a way to log a one-off backdated entry while a task is actively being tracked live without going through "Change task" / "Complete task" first — both of which end the current live session regardless (`startTask` always closes out whatever was running). `focuson log` (below) doesn't have this limitation, since it isn't gated behind the live-tracking UI at all.

This is the widget's only place besides live tracking that writes to `task_log.csv`. Keeping both write paths in the same codebase (same `CSVLogger`, same quoting/formatting) means there's exactly one implementation of "what a valid row looks like" on the widget side.

### CLI manual entry (`focuson log`)

The SwiftUI past-session entry (above) was flaky enough in practice that it's also reachable from the CLI, as `internal/tasklog.LogSession` — a second implementation of "what a valid row looks like," this time on the Go side, kept format-compatible by construction (same CSV shape, same RFC3339-UTC timestamps, same quoting rule) rather than by sharing code across languages.

- **Flags**: `focuson log --project <slug> --task "<text>" --from <time> [--to <time>] [--completed=false]`. `--from`/`--to` accept `HH:MM` (today), `"YYYY-MM-DD HH:MM"`, a duration relative to now (`-2h`, `-90m`), or a full RFC3339 timestamp; `--to` defaults to now; `--completed` defaults to true.
- **TUI**: main menu → "Log Time" → pick a project (from `manifest.toml`, like every other CLI project picker) → the same fields as a small form.
- **Validation**: the project slug must exist in `manifest.toml` and already have a `projects/<slug>/` directory (no silent directory creation). `to` must be after `from`.
- **Open-session guard**: if the project's `task_log.csv` last row has no `to` yet — the one case the startup integrity check (below) trusts to mean "a session is actively being tracked right now" — `focuson log` refuses to write at all, rather than risk burying a real in-progress widget session under a backdated row. Read-then-write happens under one `flock`, so concurrent `focuson log` runs can't race each other into both passing the check; this doesn't (and can't) guard against the widget's own unlocked append landing mid-write.
- **No auto-sync**: like every other write path here, committing to git is a separate, explicit `focuson sync` (or the daily cron job) — logging time doesn't push anything by itself.

---

## Out of Scope (v2, explicit)

- No migration of v1's `~/task_log.csv` history into the new format.
- No tax/VAT handling, no currency conversion — currency is per-client metadata only, printed on the invoice as-is.
- No invoice editing/voiding UI — fixing a bad invoice means deleting its `invoices/INV-000N.toml` (and `.pdf`) by hand and re-running generation; the next-number calculation naturally reuses that slot since it's just `max(existing) + 1`.
- No automatic git commits anywhere — `focuson sync` is always explicit.
- No multi-project invoices, no partial/split invoicing within a project.

---

## Implementation Plan

Build in dependency order: data layer first, then just enough CLI to configure it, then widget changes so real tracking can start, then the rest of the CLI (invoicing) on top of real logged data.

### Phase 1 — Data directory bootstrap ✅ done
- Define `manifest.toml` schema (this doc) and a minimal Go struct + TOML lib (e.g. `BurntSushi/toml`) for read/write.
- `focuson` CLI skeleton: directory resolution (flag or config pointing at the data dir).
- Bubble Tea app shell + main menu navigation (no real screens behind it yet).

### Phase 2 — Clients & Projects screens ✅ done
- Bubble Tea list + form components, reused across Clients and Projects screens (list/add/edit against `manifest.toml`).
- Underlying flag-free logic factored so the later CLI flags (Phase 9) are thin wrappers over the same functions.

### Phase 3 — Widget: data directory + project picker ✅ done
- Replace `focuson.logFilePath` with `focuson.dataDirectoryPath` and the directory-picker UI.
- Directory-listing-based project picker (no TOML parsing — see Widget Changes → "Project picker").
- `TaskSelectionView` project picker; `TaskStore.currentProjectSlug`.

### Phase 4 — Widget: UUID + session pairing ✅ done
- `CSVLogger.appendRow` gains `uuid`; `TaskStore` generates/stores/reuses `currentTaskUUID` across start/complete/pause/terminate paths.
- Extra correctness fix beyond the original phase description: on relaunch with a task still marked active, `loadState()` now mints a **fresh** uuid + start time rather than resuming the old one. The old session's story is already fully told on disk (closed gracefully at quit, or permanently orphaned by a crash — v1's documented limitation) — reusing its uuid would otherwise produce two `to`-filled rows sharing one uuid, breaking the one-row-per-uuid invariant invoicing depends on. User-visible effect: the elapsed-time counter resets on relaunch instead of pretending continuity across the time the app wasn't running.

### Phase 5 — Widget: manual entry ✅ done
- "Log past session" sheet (project picker, task, from/to, completed toggle) → single-row `CSVLogger.appendRow` call, independent of any active tracked task.

Phases 3–5 unblock real time-tracking with client/project awareness — real logged data to build and test invoicing against comes out of these, moved ahead of invoicing for that reason.

### Phase 6 — CLI: startup data-integrity check ✅ done
- `internal/tasklog`: `ReadRows` (parses `task_log.csv`, errors on malformed rows) and `CheckDataDirectory` (finds dangling entries — see "Startup data-integrity check" above).
- Wired into `main.go`: runs before the TUI starts, refuses to launch (prints the problem rows, exits nonzero) if anything's found.
- Added once real logged data surfaced rows that looked alarming at a glance (open/close pairs are normal; this makes the CLI itself vouch for the file before trusting it for anything else, invoicing included).

### Phase 7 — Invoice generation

Split into two parts on purpose: structured data first, PDF rendering (Phase 8) as a separate, later step reading the same data.

**Part 1 — structured invoice record ✅ done**
- `internal/invoicing`: `Preview` (the anti-join against `task_log.csv` + `invoiced.csv`, `to IS NOT NULL`, not-yet-invoiced, optionally date-bounded — pure Go, no DuckDB, see the Architecture note above) and `Commit` (Preview, then the real-run side effects: assign the next number, write `invoices/INV-000N.toml`, append billed UUIDs to `invoiced.csv`, bump `last_invoiced_at`). `SaveLedger` refuses to overwrite an existing invoice file — immutable once written.
- `NextInvoiceNumber` / `SetLast` for the Harvest-continuity placeholder scheme.
- "Invoices" TUI screen: list existing (+ read-only detail view), "Generate new invoice" (project picker → review step showing line items/total, which *is* the dry-run, nothing written until confirmed → commit), "Set starting number" form.
- Tests cover: correct sum/rate math, Preview has zero side effects, a backdated entry gets picked up on the next run regardless of date, an already-billed row never gets billed twice even with no date bound at all, empty/non-billable commits are refused, numbering continues correctly after a placeholder, and a written ledger can't be overwritten.

**Part 2 — recon + date bounds ✅ done**
- `internal/invoicing.Recon`: dup-check (a UUID billed by more than one invoice — structurally shouldn't happen given `Commit`, but free insurance against a hand-edited `invoiced.csv`) and staleness-check (a closed, billable-length row with no `invoiced.csv` entry at all, older than a threshold — the omission failure mode a pure timestamp-cursor design would've been prone to, still checked for even though `invoiced.csv` is what actually prevents it). Threshold is 14 days — recent unbilled work isn't a problem, it's just "not invoiced yet."
- "Check consistency" TUI action (third item on the Invoices screen) runs `Recon` across every billable project and lists any hits.
- A new date-bounds step between the project picker and the review screen: two optional `YYYY-MM-DD` fields, blank = unbounded (the default, "everything unbilled"). Feeds the same `invoicing.Options` the CLI flags use.

### Phase 8 — PDF rendering ✅ done
- `internal/pdfgen` (pure Go, `go-pdf/fpdf`, no external binary): business header + "INVOICE" title/number/date/period, Bill To block, a bordered line-item table (date/task/hours/amount), totals, payment-details footer. Deliberately separate from `internal/invoicing` — it only ever *reads* an `Invoice` struct, never computes one, so a PDF can always be regenerated later from an existing ledger file with zero re-derivation risk.
- Task/address/business-name text goes through `fpdf`'s UTF-8→codepage translator (`UnicodeTranslatorFromDescriptor`) before hitting the page — the core Helvetica font is single-byte-encoded, so skipping this would silently mangle anything with an accented character.
- `Commit` (Phase 7) sets `inv.PDFPath = "invoices/INV-000N.pdf"` before saving the ledger (deterministic from the number, so no chicken-and-egg problem), then the TUI's review-confirm step calls `pdfgen.Render` right after `Commit` succeeds. A PDF failure at that point doesn't roll back the already-committed invoice — it's reported as an error, and the PDF can be regenerated later from the frozen ledger data.
- Tests: output starts with a `%PDF-` header and isn't suspiciously small; renders correctly with every optional field blank; a long task description and an accented one (`Café menu page...`) both render without error.
- Verified against the user's own already-committed `INV-0046` (regenerated read-only into scratch space, `focuson-data` untouched) — 12 line items, correct totals.
- **Follow-up, after seeing a real reference invoice**: added `registration_number`, `vat_note`, and `payment_terms` to `[business]` (see manifest schema above) — printed under the business address and next to the invoice date respectively. Editable via the TUI's new **Business** screen (see Interface section) — `manifest.SetBusiness` replaces the whole block wholesale, since it's a singleton with no identity/slug to preserve.

### Phase 9 — Sync screen + scripting flags ✅ done
- `internal/gitsync`: `Status` (porcelain lines; also `git init`s the data directory on first use if it isn't a repo yet) and `Commit` (stage everything, commit — blank message gets a timestamped default). Returns a `Result{Committed, Pushed, PushError}` rather than a plain bool, since commit and push are each independently a no-op-or-not.
- **Push, added after "focuson sync is only committing, not pushing"**: `Commit` also pushes to `origin` whenever that remote exists — regardless of whether *this* call created a new commit, since a previous commit (or one made before a remote was even added) could already be sitting locally unpushed. A push failure (no network, no remote, diverged history) is reported on `PushError` but never fails the call: the local commit is real durability on its own, and a flaky network shouldn't make the daily cron job treat every run as a failure. Tested against a local bare repo standing in for a real remote (no network/GitHub dependency in tests) — covers push-on-commit, push-of-already-committed-work-on-a-clean-tree, and push-failure-doesn't-fail-commit.
- "Sync" TUI screen: shows the changed-file list (or "clean"), confirm stages+commits+pushes; reports commit and push outcomes separately, including "no origin remote configured" and "push failed, commit is still safe" as distinct non-fatal states. Pressing enter on a clean tree still attempts a push, in case something's committed-but-unpushed.
- `main.go` now has real subcommand dispatch (previously it only ever launched the TUI): `focuson sync`, `focuson invoice generate --project X [--from DATE] [--to DATE] [--dry-run]`, `focuson invoice set-last --number N [--note TEXT]` — thin wrappers calling the exact same `gitsync`/`invoicing` functions the TUI uses. The startup data-integrity check (Phase 6) now runs uniformly before any of these, not just before the TUI.
- Verified for real: the user's actual `focuson-data` repo had a remote (`origin` → a private GitHub repo) with one commit sitting locally unpushed from earlier testing. Running the fixed `focuson sync` pushed it — confirmed `git status` no longer shows "ahead of origin/main".

**Addendum — daily sync cron job**, added after "I'm going to just use this, main thing is to not lose data":
- `internal/cronsetup` installs/removes a macOS LaunchAgent (not literal `cron` — cron is unreliable on modern macOS around sleep/wake and Full Disk Access; launchd is what the OS actually uses for anything scheduled, so that's what backs the user-facing "daily cron job" framing) that runs `focuson sync` once a day.
- `focuson cron install [--time HH:MM]` (its own default 18:00 if run directly) writes `~/Library/LaunchAgents/com.focuson.dailysync.plist` and `launchctl load -w`s it; `focuson cron uninstall` unloads and removes it. Idempotent — re-running install with a new time cleanly replaces the old job.
- Refuses to install from a `go run`/`go test` temp build (detects `go-build`-in-path or a path under `os.TempDir()`) — a LaunchAgent pointing at a build-cache path that gets swept the moment the temp dir is cleaned would silently stop working forever, exactly the kind of failure this exists to prevent. Build a stable binary first (`go build -o ~/bin/focuson ./cli`), install from that.
- `install.sh` now does all three installs in one go: builds+installs the widget to `/Applications` (unchanged from v1), builds `focuson` to `~/bin/focuson` (warns if `~/bin` isn't on `PATH`, doesn't edit shell profiles itself), then runs `focuson cron install --time 00:00` — so the default schedule from a fresh install is midnight, overriding the flag's own 18:00 default.

**Widget/CLI config unification, added after "does changing the path in the widget change it [for the CLI]?"**: the widget used to keep its own copy of the data-directory path in `UserDefaults`, entirely separate from the CLI's `~/Library/Application Support/focuson/config.toml` — changing it in one place silently left the other stale. Fixed by having the widget read/write that exact same file directly (a one-line `data_dir = "..."` scan/write, not real TOML parsing — same rule the project picker follows) instead of keeping its own copy; `UserDefaults` is no longer used for this at all. `bootstrapDataDirectoryIfNeeded` persists the implicit default into that file the first time it's missing, so whichever side runs first establishes the shared value instead of each guessing its own default independently.
- Verified for real on the user's machine: installed at a test time, confirmed present in `launchctl list` and the plist content, then uninstalled and confirmed both gone — not left running, since actually scheduling something permanent on someone's machine is the user's call, not mine to leave behind unasked.

### Phase 10 — End-to-end test — skipped
Explicitly skipped per the user ("skip 10, do the rest... I'm going to just use this and see how it goes") in favor of real usage — INV-0046 and the powersync Harvest import already exercised most of this path for real anyway.
