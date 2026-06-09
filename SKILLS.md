# 📖 Plaid CLI: LLM Agent Skills & Integration Manual

Welcome, Agent! This guide defines the capabilities, command structures, data models, and integration patterns of the `plaid-cli` tool. Use this manual to interact with the tool efficiently, write scripts around it, or automate financial workflows.

---

## 🛠️ CLI Design Principles & Stdio Conventions

- **Stdout** is reserved for clean command output (JSON arrays, CSV lists, or tables).
- **Stderr** is used for diagnostic messages, status updates, prompts, and logs.
- **Agent tip**: Use `--format json` for machine-parseable output. Redirect stderr when piping: `2>/dev/null` or `2>stderr.log`.
- **Interactive terminal detection**: `configure`, `login`, `transactions` (no date flags), `accounts remove` (no arg), and `rules add` (no condition flags) will prompt when stdin is a terminal. Always supply all required flags to run headlessly.

---

## 📂 Configuration & Cache Storage

All state lives under `~/.plaid-cli/` (`C:\Users\<username>\.plaid-cli\` on Windows):

| File | Purpose |
| ------ | --------- |
| `config.json` | Plaid credentials, environment, and linked Item tokens with institution metadata. May be AES-256-GCM encrypted. |
| `cache.json` | Cached transactions, per-item sync cursors, and rule-generated overrides. May be AES-256-GCM encrypted. |
| `session.json` | Short-lived (15 min) encrypted password session — avoids re-prompting the master password between commands. Machine-keyed; not portable. |
| `rules.json` | User-defined categorization rules. Never encrypted. |

### Encrypted configs

When `secure: true` is set in `config.json`, both `config.json` and `cache.json` are AES-256-GCM encrypted on disk. Commands that read these files need the master password. Resolution order:

1. In-memory (already entered this process)
2. `PLAID_CLI_PASSWORD` environment variable
3. Session cache (`~/.plaid-cli/session.json`, expires 15 min from last use)
4. Interactive terminal prompt

**Agent tip**: Export `PLAID_CLI_PASSWORD` once per session using a silent prompt so the value never appears in your terminal or shell history:

```bash
# bash/zsh — input is not echoed
read -rs PLAID_CLI_PASSWORD && export PLAID_CLI_PASSWORD

# PowerShell
$env:PLAID_CLI_PASSWORD = (Read-Host -AsSecureString "Master password" |
  ForEach-Object { [System.Net.NetworkCredential]::new("", $_).Password })
```

All subsequent commands in the same session pick it up automatically — no inline assignment needed.

---

## 🚀 Command Reference

### 1. `configure`

Set up Plaid API credentials. Prompts interactively for any values not passed as flags. Re-running preserves existing linked Items.

| Flag | Description |
| ------ | ------------- |
| `--client-id` | Plaid Client ID |
| `--secret` | Plaid Client Secret |
| `--environment` | `sandbox` or `production` |
| `--secure` | Enable AES-256-GCM encryption for config and cache |

**Non-interactive pattern:**

```bash
.\plaid-cli.exe configure --client-id "YOUR_CLIENT_ID" --secret "YOUR_SECRET" --environment "sandbox"
```

To enable encryption, export `PLAID_CLI_PASSWORD` first (see the silent prompt pattern above), then run:

```bash
.\plaid-cli.exe configure --client-id "..." --secret "..." --environment "sandbox" --secure
```

---

### 2. `login`

Link a bank account via the browser-based Plaid Link flow. Launches a temporary local server, opens the default browser, exchanges the resulting public token for an access token, fetches institution metadata, and saves to `config.json`. Safe to run multiple times — re-linking the same institution updates the existing entry instead of duplicating it.

| Flag | Description |
| ------ | ------------- |
| `--port` | Local port for the Plaid Link page (default `8080`) |

**Sandbox credentials:** username `user_good` / password `pass_good` / MFA `1234`

---

### 3. `accounts`

Fetch and display real-time balances for all linked institutions.

Output columns: Account ID, Name, Type (Subtype), Current Balance, Available Balance, Currency.

```bash
.\plaid-cli.exe accounts
```

#### `accounts remove [item_id|account_id|number]`

Remove a linked institution. On startup, backfills any missing institution names. Accepts the target as:

- **Number**: 1-based index from the displayed list (e.g. `1`)
- **Item ID**: the Plaid item ID stored in `config.json`
- **Account ID**: any account ID owned by the target institution (resolved via Plaid)
- **No argument**: prints a numbered list and prompts interactively

On confirmation, calls Plaid `/item/remove`, removes the entry from `config.json`, purges the cursor, and deletes all cached transactions for that institution.

| Flag | Description |
| ------ | ------------- |
| `--force` | Skip confirmation prompt (for scripts) |

```bash
# Remove by list number — safest for scripts after `accounts` shows the list
.\plaid-cli.exe accounts remove 1 --force

# Remove by account ID visible in `accounts` output
.\plaid-cli.exe accounts remove acc_xxxxxxxx
```

---

### 4. `sync`

Incrementally fetch transaction changes (added, modified, removed) from Plaid and write them to `cache.json`. After saving, automatically applies all enabled rules to any new or modified transactions and reports the override count.

| Flag | Description |
| ------ | ------------- |
| `--item-id` | Sync only the specified Plaid Item ID |
| `--account-id` | Resolve to the parent item and sync only that institution |
| `--reset` | Clear cursors and re-fetch full history from scratch |

`--item-id` and `--account-id` are mutually exclusive. `--reset` scoped with either flag resets only that item's cursor and cached transactions.

```bash
# Standard incremental sync across all linked items
.\plaid-cli.exe sync

# Full historical re-fetch for one institution
.\plaid-cli.exe sync --item-id "item_sandbox_xxxxxx" --reset
```

---

### 5. `transactions`

Query and filter transactions from the local cache. Sorted date-descending. When run in a terminal with no date filter, presents a timeframe picker (30/60/90 days / all). In non-interactive mode, defaults to all transactions.

| Flag | Default | Description |
| ------ | --------- | ------------- |
| `--start-date YYYY-MM-DD` | — | Transactions on or after this date |
| `--end-date YYYY-MM-DD` | — | Transactions on or before this date |
| `--days N` | — | Last N days (mutually exclusive with `--start-date`/`--end-date`) |
| `--account-id` | — | Filter by Plaid account ID |
| `--min-amount` | — | Inclusive lower bound on amount |
| `--max-amount` | — | Inclusive upper bound on amount |
| `--search` | — | Case-insensitive substring match on transaction name |
| `--pending` | false | Show only pending transactions |
| `--limit N` | 100 | Cap the number of results |
| `--format table\|json\|csv` | table | Output format |
| `--output FILE` | stdout | Write to file instead of stdout |
| `--no-rules` | false | Show raw Plaid data; skip rule overrides |
| `--tag TAG` | — | Show only transactions carrying this override tag |
| `--ignored` | false | Show only transactions marked ignored by a rule |

```bash
# Last 30 days as JSON — fully headless
.\plaid-cli.exe transactions --days 30 --format json

# Export filtered CSV
.\plaid-cli.exe transactions --start-date "2026-01-01" --min-amount 50.00 --format csv --output spend.csv

# Show only transactions tagged "reimbursable"
.\plaid-cli.exe transactions --days 90 --tag reimbursable --format json

# Show raw Plaid data, no overrides applied
.\plaid-cli.exe transactions --days 30 --no-rules --format table
```

---

### 6. `rules`

Manage non-destructive override rules that rename, recategorize, tag, or hide transactions at render time. Rules never mutate raw Plaid data.

#### `rules list`

| Flag | Description |
| ------ | ------------- |
| `--format table\|json` | Output format (default `table`) |

#### `rules add`

Adds a rule. Any omitted condition/action fields are prompted interactively when run in a terminal. At least one condition and one action are required.

| Flag | Description |
| ------ | ------------- |
| `--name` | Human-readable rule name |
| `--match` | Case-insensitive substring match on transaction name |
| `--regex` | Go regular expression match on transaction name |
| `--account-id` | Exact match on Plaid account ID |
| `--min-amount` | Inclusive lower bound on amount |
| `--max-amount` | Inclusive upper bound on amount |
| `--category-is` | Case-insensitive substring match on Plaid's category string |
| `--rename` | Display name override |
| `--set-category` | User-defined category string |
| `--tag` | Tag to attach (repeatable flag; use multiple `--tag` for multiple tags) |
| `--ignore` | Hide transaction from budget/spend summaries |

```bash
# Regex rule — fully headless
.\plaid-cli.exe rules add \
  --name "Streaming Subscriptions" \
  --regex "(?i)(netflix|spotify|peacock)" \
  --set-category "Entertainment: Subscriptions" \
  --tag "subscription"

# Ignore a specific recurring charge by account + name
.\plaid-cli.exe rules add \
  --name "Ignore Payroll Deposit" \
  --match "DIRECT DEPOSIT" \
  --account-id "acc_xxxxxxxx" \
  --ignore
```

#### `rules remove <id>`

Delete a rule permanently.

#### `rules enable <id>` / `rules disable <id>`

Toggle a rule's active state without deleting it.

#### `rules apply`

Re-run all enabled rules against the **full** transaction cache and write overrides. Use after importing new rules or modifying existing ones.

| Flag | Description |
| ------ | ------------- |
| `--dry-run` | Print matches without writing any overrides |

```bash
.\plaid-cli.exe rules apply --dry-run
.\plaid-cli.exe rules apply
```

#### `rules test`

Dry-run a one-off condition against the cache and print matches without creating a rule. Useful for verifying a pattern before committing it.

| Flag | Description |
| ------ | ------------- |
| `--match` | Case-insensitive substring match |
| `--regex` | Go regular expression |
| `--min-amount` | Inclusive lower bound on amount |
| `--max-amount` | Inclusive upper bound on amount |

> **Note**: `rules test` supports name and amount conditions only. To test `--account-id` or `--category-is` conditions, use `rules apply --dry-run` after temporarily adding the rule via `rules add`.

```bash
.\plaid-cli.exe rules test --regex "(?i)venmo" --min-amount 50
```

---

## 🔄 Automation Workflow Examples

### Full headless setup (sandbox)

```bash
.\plaid-cli.exe configure \
  --client-id "$PLAID_CLIENT_ID" \
  --secret "$PLAID_SECRET" \
  --environment sandbox

# Login requires a browser — run manually once, then automate sync
.\plaid-cli.exe login --port 8080

.\plaid-cli.exe sync
.\plaid-cli.exe transactions --days 30 --format json
```

### Headless against an encrypted config

Export the password silently once at the start of the session (see [Encrypted configs](#encrypted-configs)), then run commands normally:

```bash
read -rs PLAID_CLI_PASSWORD && export PLAID_CLI_PASSWORD
.\plaid-cli.exe sync
.\plaid-cli.exe transactions --days 30 --format json > txns.json
```

### Apply categorization rules and export

```bash
.\plaid-cli.exe rules apply
.\plaid-cli.exe transactions \
  --start-date "2026-05-01" \
  --end-date "2026-05-31" \
  --format csv \
  --output may_2026.csv
```

### Remove a stale institution non-interactively

```bash
# List institutions first to find the number
.\plaid-cli.exe accounts

# Remove by number without confirmation
.\plaid-cli.exe accounts remove 2 --force
```

---

## 🧪 Sandbox Testing Notes

- Sandbox environment: `--environment sandbox`
- Credentials: `user_good` / `pass_good` / MFA `1234`
- Any institution can be selected; all account types (checking, savings, credit card, investment) are simulated instantly.
- To test re-linking flows, trigger `ITEM_LOGIN_REQUIRED` by using sandbox error credentials.
- `--reset` on `sync` is useful in sandbox to replay the full historical fetch.
