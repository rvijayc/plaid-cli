# 📖 Plaid CLI: LLM Agent Skills & Integration Manual

Welcome, Agent! This guide defines the capabilities, command structures, data models, and integration patterns of the `plaid-cli` tool. Use this manual to interact with the tool efficiently, write scripts around it, or automate financial workflows.

---

## 🛠️ CLI Design Principles & Stdio Conventions

To automate and integrate this tool successfully:
*   **Separation of Stdout/Stderr**:
    *   **Stdout** is reserved exclusively for clean command output (e.g. JSON arrays, CSV lists, or tables).
    *   **Stderr** is used for diagnostic messages, status updates, prompts, headers, and logs.
    *   *Agent Tip*: When parsing tool outputs in pipelines, safely redirect Stderr (`2>/dev/null` or `2>stderr.log`) or use specific format flags (like `--format json`) to extract clean, parseable payloads from Stdout.
*   **Interactive Terminal Detection**:
    *   Some commands (e.g. `configure`, `login`, and `transactions` without timeframe flags) will prompt the user if they detect a terminal on Stdin.
    *   *Agent Tip*: Always specify all required flags (`--client-id`, `--secret`, `--environment`, `--days`, `--format`) to run in a fully headless, non-interactive mode.

---

## 📂 Configuration & Cache Storage

The CLI stores its settings and caches locally in the user's home directory under `~/.plaid-cli/` (e.g. `C:\Users\<username>\.plaid-cli\` on Windows, `/home/<username>/.plaid-cli/` on Unix/Mac):

1.  **Configuration**: `~/.plaid-cli/config.json`
    *   Stores your client ID, secret, environment selection, and list of linked Plaid Items (institutions) with their respective access tokens.
2.  **Local Cache**: `~/.plaid-cli/cache.json`
    *   Caches incremental transaction records and synchronization cursors per Item.

---

## 🚀 Command Reference & Agent Usage Patterns

### 1. `configure`
Sets up Plaid API credentials.
*   **Flags**:
    *   `--client-id` : Plaid Client ID (from Plaid Dashboard)
    *   `--secret` : Plaid Client Secret (Sandbox, Development, or Production secret)
    *   `--environment` : The Plaid environment (`sandbox`, `production`, or `development`)
*   **Agent Non-Interactive Pattern**:
    ```bash
    .\plaid-cli.exe configure --client-id "YOUR_CLIENT_ID" --secret "YOUR_SECRET" --environment "sandbox"
    ```

### 2. `login`
Links a bank account/institution using the interactive Plaid Link Web Flow.
*   **Mechanism**: Launches a local web server (defaults to port `8080`) and automatically opens the default web browser.
*   **Flags**:
    *   `--port` : Local port to run the Plaid Link page (default: `8080`)
*   **Agent Automation Pattern**:
    1.  Launch the command in the background or prepare to orchestrate:
        ```bash
        .\plaid-cli.exe login --port 8080
        ```
    2.  If headless, use a browser/automation tool to navigate to `http://localhost:8080`.
    3.  Follow the Link UI: select any bank, authenticate in Sandbox using `user_good` / `pass_good` (MFA `1234`), and submit. The local server will capture the public token, exchange it, and save the access token to `config.json`.

### 3. `sync`
Incrementally fetches new, modified, or deleted transaction data from Plaid and updates the local cache.
*   **Flags**:
    *   `--reset` : Clears the local transaction history and sync cursors, prompting a full historical sync.
    *   `--item-id` : Limits syncing to a specific Plaid Item ID.
    *   `--account-id` : Limits syncing to the Item containing a specific Plaid Account ID.
*   **Agent Usage Pattern**:
    *   *Standard incremental sync*:
        ```bash
        .\plaid-cli.exe sync
        ```
    *   *Resync a specific item*:
        ```bash
        .\plaid-cli.exe sync --item-id "item_sandbox_xxxxxx"
        ```

### 4. `accounts`
Fetches and lists balances and details of all linked accounts in real time.
*   **Outputs**: Account ID, account name, type, current balance, available balance, and currency.
*   **Agent Usage Pattern**:
    ```bash
    .\plaid-cli.exe accounts
    ```

### 5. `transactions`
Queries, filters, and formats transactions cached locally.
*   **Flags**:
    *   `--start-date` : Filter starting date (YYYY-MM-DD).
    *   `--end-date` : Filter ending date (YYYY-MM-DD).
    *   `--days` : Quick filter for the last N days (e.g. `30`, `90`). *Mutually exclusive with `--start-date` and `--end-date`.*
    *   `--account-id` : Filter by a specific Account ID.
    *   `--search` : Search names (case-insensitive substring match).
    *   `--min-amount` / `--max-amount` : Filter amount ranges.
    *   `--pending` : Show only pending transactions.
    *   `--limit` : Max records to display (default: 100).
    *   `--format` : Output format: `table` (default), `json`, or `csv`.
    *   `--output` : Write result directly to a file.
*   **Agent Non-Interactive Patterns**:
    *   *Extract all transactions in the last 30 days as JSON*:
        ```bash
        .\plaid-cli.exe transactions --days 30 --format json
        ```
    *   *Export filtered transactions to a CSV file*:
        ```bash
        .\plaid-cli.exe transactions --start-date "2026-01-01" --min-amount 50.00 --format csv --output spend_report.csv
        ```

---

## 🧪 Testing in the Sandbox Environment

To test features and pipeline validation safely:
1.  Initialize sandbox credentials:
    ```bash
    .\plaid-cli.exe configure --client-id "test_client" --secret "test_secret" --environment "sandbox"
    ```
2.  Log in using sandbox parameters. If you are simulating a link flow in the browser, use:
    *   **Username**: `user_good`
    *   **Password**: `pass_good`
    *   **MFA**: Any numeric code (e.g., `1234`)
3.  Simulate errors or re-auth by triggering standard Plaid sandbox login failures when testing re-linking workflows.
