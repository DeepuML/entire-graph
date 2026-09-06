# Entire ChangeRadar: Graph-Powered PR Risk Radar & Smart Test Selector

## One-sentence summary
Entire ChangeRadar combines semantic git diffs with Entire Graph's deep caller/callee snapshot index to deliver instant (sub-10ms) pull request risk scoring (0–100), transitive blast-radius mapping, untested blindspot detection, evidence tiering (Confirmed vs. Heuristic vs. Unverified Claims), and automated smart test suite selection.

---

## Problem, intended user and why it matters
- **Intended User:** Software engineers, code reviewers, DevOps leads, and autonomous AI coding agents navigating multi-thousand file codebases.
- **The Problem:** 
  - **`git diff` is blind to software architecture:** Standard diffs only reveal raw text line additions and deletions. They do not answer: *"What upstream callers broke across packages?", "Does this private helper touch a critical production API route?", or "Which specific tests validate this change?"*
  - **Slow, expensive CI regression runs:** Full regression test suites often take 20–45 minutes. Developers either wait forever for CI feedback or skip running tests locally due to friction.
  - **Silent production regressions:** Developers modify internal functions assuming no side effects, unknowingly breaking callers two or three layers up the call graph.
  - **Static analysis over-confidence:** Real codebases use reflection, dynamic dispatch, and generated code (`*.pb.go`). Treating static call graphs as absolute truth creates a false sense of security.
- **Why it matters:**
  ChangeRadar turns raw code diffs into actionable architectural intelligence. By traversing precomputed tree-sitter call graphs in milliseconds, developers and AI agents immediately get:
  1. An objective **PR Risk Score (0–100)** to triage reviews.
  2. The exact **transitive blast radius** showing all affected callers.
  3. **Untested Blindspot warnings** where active callers depend on code lacking unit test coverage.
  4. **Smart Test Selection** generating the exact narrow test commands needed (`go test -run ...`).
  5. **Evidence Tiering** distinguishing verified static AST edges from heuristic dynamic dispatch.

---

## Selected Entire track and why Entire is essential
- **Selected Track:** **Track 2 — Build with Graph Intelligence**
- **Why Entire is Essential:**
  - Track 2 explicitly calls for:
    - *"Impact-aware code review or change-risk analysis"*
    - *"Test selection based on affected relationships"*
    - *"Combining Entire Graph findings with checkpoint intent"*
    - *"Raw graph output is not enough. Your product should produce a useful decision, recommendation or workflow, show where the evidence came from, and let the user verify it against source code or tests."*
  - **Why Entire Graph makes this possible:**
    - Without Entire Graph, calculating transitive caller chains across Go packages would require cold AST parsing across the entire repository on every commit (taking seconds to minutes).
    - Entire Graph precomputes the repository structure into a local SQLite snapshot (`entire-graph.db`).
    - ChangeRadar queries this snapshot locally with **zero network egress** and **zero external API costs**, returning full blast-radius analyses in **under 10 milliseconds**.

---

## Architecture and main workflow

```
                   Git Revision Range (Base .. Head)
                                  │
                                  ▼
                 ┌───────────────────────────────────┐
                 │       Semantic Diff Engine        │  (Extracts modified symbols,
                 │        (Tree-sitter AST)          │   types, and line ranges)
                 └─────────────────┬─────────────────┘
                                  │
                                  ▼
                 ┌───────────────────────────────────┐
                 │    Entire Graph Local Index       │  (Precomputed SQLite snapshot;
                 │    (Callers, Callees, Routes)     │   queries callers & depth <=2)
                 └─────────────────┬─────────────────┘
                                  │
                                  ▼
                 ┌───────────────────────────────────┐
                 │       Risk & Blast Radius         │  (Calculates 0-100 score,
                 │       - Caller Fan-out            │   cross-package leakage,
                 │       - Route Exposure            │   and blindspot penalties)
                 │       - Test Mitigation           │
                 └─────────────────┬─────────────────┘
                                  │
                                  ▼
                 ┌───────────────────────────────────┐
                 │     Track 2 Curveball Engine      │  (Categorizes Confirmed AST vs
                 │     "Graph is Evidence"           │   Heuristic vs Unverified Claims;
                 │     - Reflection & Gen Code       │   triggers Safe Fallback suite)
                 └─────────────────┬─────────────────┘
                                  │
                                  ▼
                 ┌───────────────────────────────────┐
                 │       Smart Test Selector         │  (Synthesizes exact narrow
                 │                                   │   test commands: go test ...)
                 └─────────────────┬─────────────────┘
                                  │
         ┌────────────────────────┼────────────────────────┐
         ▼                        ▼                        ▼
   [Terminal CLI]         [GitHub Actions CI]      [AI Coding Agent]
   (--format text)          (PR Review Bot)         (--format json)
```

### Core Components Implemented:
1. **`internal/cli/changeradar.go`**: Core engine integrating semantic diff extraction with Entire Graph traversal, multi-factor risk scoring, evidence tiering, and test selection.
2. **Multi-Factor Risk Scoring Engine (0–100)**:
   - `0 - 29 (LOW)`: Localized private helper, few callers, fully tested.
   - `30 - 59 (MEDIUM)`: Package-level entity with moderate caller fan-out.
   - `60 - 79 (HIGH)`: Public exported API or core dispatcher with transitive callers.
   - `80 - 100 (CRITICAL)`: Foundational interface, deletion of widely used symbols, or root CLI dispatcher affecting multiple subsystems.
3. **Untested Blindspot Detector**: Flags symbols that have active callers depending on them, but **zero** unit tests exercising them (+15 risk penalty).
4. **Smart Test Command Synthesizer**: Scans affected symbol paths and caller locations to output executable test commands (e.g. `go test ./internal/cli -run '^TestParseChangeRadarFlags$'`). Supports Go, Python (`pytest`), TypeScript/JavaScript (`npm test`), and Rust (`cargo test`).
5. **Multiple Consumption Formats**:
   - `--format text`: Rich colored console summary with risk badges, blast radius breakdown, and next actions.
   - `--format markdown`: Clean markdown table and collapsible details ready for GitHub PR automated comments.
   - `--format json`: Structured data for CI gating and AI agent tool consumption.

---

## Entire Graph findings and verification

### 1. Verification on `entire-graph` Itself
ChangeRadar was verified directly against `entire-graph`'s own repository. When testing modifications to `internal/cli/root.go:dispatchCommands`:
- **Direct Callers Identified:** `Execute()`, `RootCmd.RunE`
- **Transitive Callers Identified:** `main.main()` in `cmd/entire-graph/main.go`
- **Affected Tests Identified:** `internal/cli/help_test.go`, `internal/cli/root_test.go`
- **Execution Benchmark:** **8.4 milliseconds** end-to-end.

### 2. Live Terminal Command
```powershell
.\entire-graph.exe changeradar --base HEAD~1 --head HEAD --format text
```

---

## Noon Curveball: what changed and how we adapted

- **Pre-Noon Stable State Preserved (11:45 AM):**
  - Commit SHA: `84f19dcb`
  - Checkpoint 2 created preserving core architecture, 100% passing tests, and CLI dispatcher.

- **Received Constraint (12:00 PM):**
  > **TRACK 2: GRAPH IS EVIDENCE, NOT AN ORACLE**  
  > *"Your Graph-powered experience has encountered a repository using dynamic dispatch, generated code, reflection, or another pattern that static analysis cannot fully resolve. The product must not present incomplete Graph relationships as certain. It must identify when analysis may be partial, provide a safe fallback or verification path, and let users/agents distinguish confirmed structural evidence, heuristic/incomplete evidence, and unverified claims."*

- **How We Adapted & Solved the Constraint:**
  1. **Impact Analysis First:** Used `entire-graph impact` on `runChangeRadar` to identify all callers and consumers before modifying code.
  2. **Three-Tier Evidence Classification (`EvidenceTier`):**
     - `CONFIRMED_STRUCTURAL`: Direct static AST call graph match verified by tree-sitter.
     - `HEURISTIC`: Inferred via dynamic dispatch, Go reflection (`reflect.ValueOf`, dynamic method lookup), or auto-generated files (`*_gen.go`, `*.pb.go`).
     - `UNVERIFIED_CLAIM`: Changed symbols with 0 static callers found in the graph (may be invoked dynamically or via external packages).
  3. **Analysis Completeness Engine (`AnalysisCompleteness`):**
     - Automatically reports `Status` (`COMPLETE` vs. `PARTIAL`).
     - Computes an explicit `ConfidenceScore` (1.0 for complete AST, degraded to 0.50–0.75 for dynamic patterns).
     - Emits transparent reason strings explaining why static analysis is degraded.
  4. **Safe Fallback & Verification Path:**
     - When analysis is partial, ChangeRadar automatically surfaces a safe fallback test command:
       `go test -v -race ./... && go vet ./...`
     - Prompts code reviewers and AI agents to manually inspect reflection registries and dynamic interface dispatch sites.
  5. **Dedicated Partial Analysis Fixtures:**
     - `internal/cli/testdata/partial_analysis/dynamic_dispatch.go`: Demonstrates runtime reflection and dynamic string-based handler registration.
     - `internal/cli/testdata/partial_analysis/api_gen.go`: Demonstrates auto-generated protobuf schema structs.
  6. **Unit Test Verification:**
     - Created `internal/cli/buildathon_track2_test.go` with 8 comprehensive subtests verifying all requirements (**100% PASS**).

---

## Checkpoint links and what each checkpoint proves

| Checkpoint | Milestone | What It Proves | Commit SHA |
|------------|-----------|----------------|------------|
| **CP 1** | Initial Understanding & Architecture | Problem framing, Track 2 design document, schema mapping between semantic diffs and SQLite graph snapshots | Initial Design |
| **CP 2** | Pre-Noon Stable State (11:45 AM) | Complete working `changeradar` CLI, 100% passing unit tests, CI action, and help registry | `84f19dcb` |
| **CP 3** | Noon Curveball Response (12:00 PM) | Absorption of surprise constraint: 3-tier evidence classification, reflection detection, safe fallback verification, test fixtures | `2803e234` |
| **CP 4** | Final Verification & CI Integration | 8/8 comprehensive subtests pass, GitHub Actions PR bot active, formatted with `gofmt -s` | `2778fa9f` |

---

## Setup, run and test instructions

### Prerequisites:
- Go 1.22+ installed
- 64-bit C compiler (e.g. MinGW-w64 GCC / MSYS2 UCRT64) for tree-sitter CGo compilation

### 1. Build the binary
```powershell
# Windows helper script (sets 64-bit MinGW GCC automatically):
.\build.bat

# Or manual build:
go build -o entire-graph.exe ./cmd/entire-graph
```

### 2. Run unit tests & verification suite
```powershell
# Windows helper script (runs the full Track 2 comprehensive suite):
.\test.bat

# Or run via go test directly:
go test -v ./internal/cli -run TestBuildathonTrack2ComprehensiveSuite
go test -v ./internal/cli -run TestTrack2CurveballIncompleteAnalysis
```

### 3. Run ChangeRadar on your repository
```powershell
# Analyze working tree changes vs HEAD:
.\entire-graph.exe changeradar

# Analyze changes between two commits or branches:
.\entire-graph.exe changeradar --base HEAD~1 --head HEAD --format text

# Generate a GitHub Actions PR summary in Markdown:
.\entire-graph.exe changeradar --base origin/main --head HEAD --format markdown

# Machine-readable JSON output for AI agents and CI gating:
.\entire-graph.exe changeradar --base origin/main --head HEAD --format json

# Enforce a strict risk gate in CI (fails with code 1 if risk exceeds threshold):
.\entire-graph.exe changeradar --base origin/main --head HEAD --min-risk critical --strict
```

---

## Databricks use, data sources and limitations (if applicable)
*(Not opted into the Databricks track; focused 100% on winning Track 2: Graph Intelligence)*

**Enterprise Vision & Roadmap:**  
ChangeRadar exposes structured JSON telemetry (`--format json`). In an enterprise deployment, these PR risk logs, blast-radius metrics, and blindspot trends can stream directly into a **Databricks Lakehouse (Delta Lake)**. From there, engineering leadership can use Databricks Dashboards and Genie to track repository risk heatmaps, architectural drift, and test debt across thousands of microservices.

---

## Known limitations and next steps
1. **Dynamic Language Reflection:** In Python and JavaScript, dynamic reflection and runtime `eval()` cannot be resolved statically by AST trees; ChangeRadar transparently flags dynamic calls as `TierHeuristic` and requires test verification.
2. **Next Steps:**
   - Pre-push Git hook warning developers when a commit has a Risk Score > 70.
   - IDE hover extension (VS Code / JetBrains) displaying the live blast radius directly inside the editor before saving.
   - Cross-repository graph federation for distributed microservice architectures.
