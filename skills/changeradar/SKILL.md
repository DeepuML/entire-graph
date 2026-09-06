---
name: changeradar
description: >
  Analyze code changes across a branch or pull request using Entire Graph's semantic
  engine. Evaluates blast radius, detects affected API routes and cross-module callers,
  calculates a 0-100 risk score, flags untested blindspots, and selects the exact test
  suite to run before merging. Use when reviewing pull requests, checking PR risk,
  finding which tests to run, or assessing the impact of changes.
---

# entire graph changeradar

Use `entire graph changeradar` to inspect the blast radius and regression risk of code changes.
It combines `diff` (changed functions, types, and methods) with `impact` (transitive callers,
type consumers, and API routes) into a single, bounded report.

## When to use

Run ChangeRadar when:
- **Reviewing a Pull Request or Branch:** "What is the blast radius of this branch?"
- **Test Selection:** "Which tests must I run to verify my changes?"
- **Pre-merge Risk Assessment:** "Are there any breaking changes or untested blindspots?"
- **Refactoring:** Checking whether altering a signature broke unexpected callers.

## Command Invocations

```sh
# Terminal text report (default: compares main vs HEAD)
entire graph changeradar --repo .

# Compare custom branches
entire graph changeradar --repo . --base main --head feature-branch

# Markdown format (ideal for GitHub PR comments / agent summaries)
entire graph changeradar --repo . --format markdown

# JSON format filtered to high and critical risk items
entire graph changeradar --repo . --format json --min-risk high
```

## How to Interpret the Output

1. **Overall Risk Score & Level**:
   - `LOW` (0–29): Isolated changes or new entities with verified test coverage. Safe to merge.
   - `MEDIUM` (30–59): Moderate fanout. Run the recommended test suite before merging.
   - `HIGH` (60–79): Touches public API routes, signatures, or has significant cross-file callers.
   - `CRITICAL` (80–100): High blast-radius core modifications or breaking changes. Review thoroughly.

2. **⚠️ Untested Blindspots**:
   If a symbol has active callers in the codebase but **zero covering unit tests**, ChangeRadar
   flags it as a blindspot. Prioritize adding unit tests for these symbols before merging!

3. **🧪 Recommended Test Suite**:
   ChangeRadar automatically maps every impacted symbol to its covering tests in the graph and
   synthesizes the narrowest runnable test commands (e.g. `go test ./<pkg> -run '^TestName$'`).
   Always run this suite to confirm zero regressions.
