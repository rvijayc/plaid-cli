# Agent Instruction Manual: Feature Planning & Implementation Guide

Welcome, AI Coding Agent! This guide outlines the standardized engineering workflow and design patterns you must follow when planning and implementing new features in the `plaid-cli` repository.

---

## 🛠️ Phase 1: Research & Codebase Onboarding

Before writing any code, perform a comprehensive inspection of the current workspace:

1.  **Read the Specs**:
    *   Review [README.md](file:///C:/Users/igots/Documents/antigravity/plaid-cli/README.md) to understand installation, configuration, and commands.
    *   Review [SPEC.md](file:///C:/Users/igots/Documents/antigravity/plaid-cli/SPEC.md) to understand the architecture, data schemas, and the feature roadmap.
    *   **Research Plaid API Docs**: Refer to the official [Plaid API Documentation](https://plaid.com/docs/api/) to understand Plaid products, rates, error responses, and parameter formats. Always double-check struct definitions and methods within the specific Go library version (e.g. `github.com/plaid/plaid-go/v20/plaid`) by running `go doc` queries.
2.  **Inspect Key Modules**:
    *   [pkg/config/config.go](file:///C:/Users/igots/Documents/antigravity/plaid-cli/pkg/config/config.go): Config/Cache models and multi-item schemas.
    *   [pkg/client/plaid.go](file:///C:/Users/igots/Documents/antigravity/plaid-cli/pkg/client/plaid.go): Plaid API Client initialization and wrappers.
    *   [cmd/](file:///C:/Users/igots/Documents/antigravity/plaid-cli/cmd/): Cobra command route files.
3.  **Validate Build State**:
    *   Ensure the existing code builds cleanly before you start modifying files:
        ```bash
        go build -o plaid-cli.exe
        ```

---

## 📋 Phase 2: Planning Your Feature

Develop a detailed design plan before modifying source files. Create or update your internal task tracker and document:
*   **Target Components**: Which package files must be edited or created?
*   **Schema Changes**: If you need new configuration values or cached objects, update the models in `pkg/config/config.go`. Keep them backward compatible!
*   **Error Handling**: Go handles errors explicitly. Design clear error-wrapping formats.
*   **Interface Output Conventions**:
    *   **CRITICAL**: Separation of Stdout and Stderr is strictly enforced.
    *   *Stdout*: Reserved 100% clean for command outputs (e.g. JSON arrays or CSV files).
    *   *Stderr*: Used for user prompts, headers, warnings, diagnostic logs, and status percentages.

---

## 💻 Phase 3: Implementation Guidelines

When writing the Go source files, adhere to these principles:

### 1. Maintain Clean Architecture
Keep code decoupled:
- **CLI layer** (`cmd/`): Cobra routing, input validation, flags, prompting, and output layout formatting.
- **Service layer** (`pkg/client/` or other domain packages): Business logic and third-party APIs.
- **Config layer** (`pkg/config/`): Reading, writing, and migrating configurations.

### 2. Follow Go Idioms
- Keep comments and docstrings intact.
- Handle all returned errors:
  ```go
  result, err := someFunction()
  if err != nil {
      return fmt.Errorf("descriptive context: %w", err)
  }
  ```
- Use `github.com/plaid/plaid-go/v20` structures correctly. Refer to the Plaid Go SDK docs via `go doc` if you are unsure of struct constructors.

### 3. Ensure Auto-Migration for Configs
If your feature modifies `config.json` or `cache.json` fields:
- Always preserve legacy fields (keep them `omitempty` in structs).
- Implement an auto-migration function inside `LoadConfig()` or `LoadCache()` that populates the new fields from legacy formats and writes back to disk.

---

## 🧪 Phase 4: Verification & Handshake

Verify and document your work before concluding your task:
1.  **Format and Lint**:
    *   Run `go fmt ./...` and `go vet ./...` to guarantee code style matches Go standard conventions.
2.  **Compile**:
    *   Run `go build -o plaid-cli.exe` to verify successful compilation.
3.  **Manual Test Run**:
    *   Test command-line behaviors using the sandbox environment.
    *   Verify that JSON outputs are valid JSON without terminal prompts leaking into the stdout pipe:
        ```bash
        .\plaid-cli.exe transactions --format json | jq .
        ```
4.  **Update Artifacts**:
    *   Update [task.md](file:///C:/Users/igots/.gemini/antigravity/brain/1de591ce-621f-46c6-8548-8196444c50f8/task.md) and [walkthrough.md](file:///C:/Users/igots/.gemini/antigravity/brain/1de591ce-621f-46c6-8548-8196444c50f8/walkthrough.md) documenting exactly what was implemented, how it was verified, and instructions for manual testing.
