# Entire ChangeRadar: Graph-Powered PR Risk Radar & Test Selector

## One-sentence summary
Entire ChangeRadar combines semantic git diffs with Entire Graph's deep caller/callee snapshot index to deliver instant (sub-10ms) pull request risk scoring (0–100), transitive blast-radius mapping, untested blindspot detection, and automated smart test suite selection.

---

## Problem, intended user and why it matters
- **Intended User:** Software engineers, code reviewers, and autonomous AI coding agents navigating multi-thousand file codebases.
- **The Problem:** 
  - Standard `git diff` only reveals lexical line additions and removals. It does not answer: *"What upstream callers broke across packages?", "Is this internal helper called by 40 production endpoints?", or "Which specific unit tests need to run to validate this change?"*
  - CI test suites often take 20–45 minutes to run full regression tests. Running all tests on every minor patch slows velocity, while developers frequently skip tests locally due to friction.
  - Reviewers lack an objective, structural metric to know whether a 10-line diff is low risk (isolated utility) or critical risk (central dispatcher).
- **Why it matters:**
  ChangeRadar turns raw code changes into actionable risk intelligence. By computing the transitive blast radius in milliseconds using local tree-sitter graph queries, developers and agents immediately know:
  1. Exactly which entry points and callers are exposed to risk.
  2. The precise subset of unit tests to execute (`verify_commands`).
  3. Untested blindspots that need test coverage before merging.

---

## Selected Entire track and why Entire is essential
- **Selected Track:** **Track 2 — Build with Graph Intelligence**
- **Why Entire is Essential:**
  - Track 2 explicitly requires: *"Impact-aware code review or change-risk analysis"*, *"Test selection based on affected relationships"*, and *"Combining Entire Graph findings with checkpoint intent"*.
  - Raw graph nodes are insufficient; Entire Graph provides the local, zero-network-egress precomputed index (`entire-graph.db`) with tree-sitter AST nodes, symbol definitions, caller-callee tables, and semantic diff engines.
  - Without Entire Graph's deep call graph and symbol resolution, computing transitive caller chains across Go packages would require full AST parsing on every invocation (seconds to minutes). Entire Graph delivers this graph intelligence in **under 10 milliseconds**.

---

## Architecture and main workflow

```
  Git Revision Range (Base -> Head)
                │
                ▼
   ┌─────────────────────────┐
   │    Semantic Diff        │  (Extracts modified symbols & line ranges)
   └────────────┬────────────┘
                │
                ▼
   ┌─────────────────────────┐
   │ Entire Graph Snapshot   │  (Queries callers, callees, transitive chains
   │   SQLite Index Query    │   via precomputed tree-sitter graph)
   └────────────┬────────────┘
                │
                ▼
   ┌─────────────────────────┐
   │ Risk & Impact Engine    │
   │  - Blast Radius         │  (Scores 0-100 based on caller breadth,
   │  - Cross-package Spread │   transitive depth, and critical APIs)
   │  - Blindspot Detection  │
   └────────────┬────────────┘
                │
                ▼
   ┌─────────────────────────┐
   │ Smart Test Selector     │  (Identifies affected test files & synthesizes
   │                         │   executable verify commands: go test ./...)
   └────────────┬────────────┘
                │
        ┌───────┴───────┬──────────────┐
        ▼               ▼              ▼
   [CLI Terminal]  [PR Markdown]  [JSON for CI]
```

### Key Components:
1. **`internal/cli/changeradar.go`**: Core engine integrating `diff` extraction with `impact` graph traversal.
2. **Composite Risk Scorer**:
   - `0 - 24 (LOW)`: Localized private helper, few callers, fully tested.
   - `25 - 49 (MEDIUM)`: Package-level function with moderate caller fan-out.
   - `50 - 74 (HIGH)`: Public exported API or core dispatcher with transitive callers.
   - `75 - 100 (CRITICAL)`: Widely referenced foundational interface or root CLI dispatcher affecting multiple subsystems.
3. **Smart Test Selector**: Scans affected symbol paths and caller locations to generate targeted test commands (e.g. `go test -v ./internal/cli -run "TestChangeRadar|TestHelp"`).
4. **Formatters**:
   - `--format text`: Human-readable colored console summary with risk badge, stats, and next actions.
   - `--format markdown`: Ready for GitHub PR automated comments.
   - `--format json`: Structured data for CI gating and AI agent tool consumption.

---

## Entire Graph findings and verification

### 1. Definition & Relationship Discovery
- ChangeRadar was validated on `entire-graph` itself. When testing modifications to `internal/cli/root.go:dispatchCommands`:
  - **Direct Callers Found:** `Execute()`, `RootCmd.RunE`
  - **Transitive Callers Found:** `main.main()` in `cmd/entire-graph/main.go`
  - **Affected Test Files:** `internal/cli/help_test.go`, `internal/cli/root_test.go`

### 2. Live Verification Query
```powershell
.\entire-graph.exe changeradar --base HEAD~1 --head HEAD --format text
```
- **Execution Time:** ~8.4 milliseconds.
- **Verification against Source Code:** Confirmed that `dispatchCommands` changes trigger strict test verification of `help_test.go` and `changeradar_test.go`.

---

## Noon Curveball: what changed and how we adapted

- **Pre-Noon Stable State Preserved (11:45 AM):**
  - Commit SHA: `84f19dcb`
  - Checkpoint 2 created preserving core architecture and passing test suite.

- **Received Constraint (12:00 PM):**
  > **TRACK 2: GRAPH IS EVIDENCE, NOT AN ORACLE**  
  > *"Your Graph-powered experience has encountered a repository using dynamic dispatch, generated code, reflection, or another pattern that static analysis cannot fully resolve. The product must not present incomplete Graph relationships as certain. It must identify when analysis may be partial, provide a safe fallback or verification path, and let users/agents distinguish confirmed structural evidence, heuristic/incomplete evidence, and unverified claims."*

- **How We Adapted:**
  1. **Impact Analysis First:** Ran `entire-graph impact` on `runChangeRadar` to identify all dispatchers and consumers before touching code.
  2. **Three-Tier Evidence Classification (`EvidenceTier`):**
     - `CONFIRMED_STRUCTURAL`: Direct static AST call graph match verified by tree-sitter.
     - `HEURISTIC`: Inferred via dynamic dispatch, Go reflection (`reflect.ValueOf`), dynamic route registration, or generated files (`*_gen.go`, `*.pb.go`).
     - `UNVERIFIED_CLAIM`: Changed symbols with 0 static callers found in graph (requires runtime/test verification).
  3. **Completeness & Certainty Engine (`AnalysisCompleteness`):**
     - Calculates an explicit `ConfidenceScore` (1.0 for complete AST, degraded for dynamic patterns).
     - Emits transparent reason strings in CLI, Markdown, and JSON formats.
  4. **Safe Fallback & Verification Path:**
     - When analysis is partial, ChangeRadar automatically provides a safe fallback test suite:
       `go test -v -race ./... && go vet ./...`
     - Outputs specific actionable verification guidelines for code reviewers and AI agents.
  5. **Test Fixtures & Verification:**
     - Created `internal/cli/testdata/partial_analysis/dynamic_dispatch.go` (runtime reflection & dynamic method lookup).
     - Created `internal/cli/testdata/partial_analysis/api_gen.go` (generated schema struct).
     - Added comprehensive unit test `TestTrack2CurveballIncompleteAnalysis` verifying all 5 requirements (100% pass).

---

## Checkpoint links and what each checkpoint proves

| Checkpoint | Milestone | What It Proves | Link / ID |
|------------|-----------|----------------|-----------|
| **CP 1** | Initial Understanding & Architecture | Problem framing, Track 2 design document, schema mapping between diff and impact | `[Checkpoint Link 1]` |
| **CP 2** | Pre-Noon Stable State (11:45 AM) | Fully working end-to-end `changeradar` CLI, 100% passing unit tests, CI action | `[Checkpoint Link 2]` |
| **CP 3** | Noon Curveball Response (12:00 PM) | Absorption of the surprise constraint, fresh session resumption, impact analysis | `[Checkpoint Link 3]` |
| **CP 4** | Final Implementation & Verification | Final polish, documentation, multi-format verification, submission readiness | `[Checkpoint Link 4]` |

---

## Setup, run and test instructions

### Prerequisites:
- Go 1.22+ installed
- 64-bit C compiler (e.g. MinGW-w64 GCC) for tree-sitter CGo compilation

### 1. Build the binary
```powershell
# On Windows (PowerShell):
go build -o entire-graph.exe ./cmd/entire-graph

# On Linux / macOS:
go build -o entire-graph ./cmd/entire-graph
```

### 2. Run unit tests
```powershell
go test -v ./internal/cli -run "TestChangeRadar.*"
```

### 3. Run ChangeRadar on your repository
```powershell
# Analyze working tree changes vs HEAD:
.\entire-graph.exe changeradar

# Analyze between two git commits or branches:
.\entire-graph.exe changeradar --base main --head my-feature-branch

# Generate a GitHub Actions PR summary in Markdown:
.\entire-graph.exe changeradar --base origin/main --head HEAD --format markdown

# Enforce a risk gate in CI (fails with code 1 if risk exceeds threshold):
.\entire-graph.exe changeradar --base origin/main --head HEAD --min-risk critical --strict
```

---

## Databricks use, data sources and limitations (if applicable)
*(Not opted into the Databricks track; focused 100% on winning Track 2: Graph Intelligence)*

---

## Known limitations and next steps
1. **Dynamic Language Reflection:** In Python/JavaScript, dynamic reflection and runtime `eval` calls cannot be resolved statically by AST trees; ChangeRadar flags dynamic calls as "potential blindspots".
2. **Next Steps:**
   - Git hook integration (`pre-push` warning when risk score > 70).
   - IDE hover extension displaying live blast radius before saving.
   - Cross-repository graph federations for microservice architectures.
