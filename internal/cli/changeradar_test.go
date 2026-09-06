package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/entireio/entire-graph/internal/sem"
)

func TestParseChangeRadarFlags(t *testing.T) {
	// Defaults
	flags, err := parseChangeRadarFlags([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.Format != "text" {
		t.Errorf("expected format 'text', got %q", flags.Format)
	}
	if flags.Depth != 2 {
		t.Errorf("expected depth 2, got %d", flags.Depth)
	}
	if flags.MinRisk != "all" {
		t.Errorf("expected min-risk 'all', got %q", flags.MinRisk)
	}

	// Custom flags
	custom, err := parseChangeRadarFlags([]string{
		"--base", "origin/main",
		"--head", "feature-branch",
		"--format", "markdown",
		"--depth", "3",
		"--min-risk", "high",
		"--worktree",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if custom.Base != "origin/main" {
		t.Errorf("expected base 'origin/main', got %q", custom.Base)
	}
	if custom.Head != "feature-branch" {
		t.Errorf("expected head 'feature-branch', got %q", custom.Head)
	}
	if custom.Format != "markdown" {
		t.Errorf("expected format 'markdown', got %q", custom.Format)
	}
	if custom.Depth != 3 {
		t.Errorf("expected depth 3, got %d", custom.Depth)
	}
	if custom.MinRisk != "high" {
		t.Errorf("expected min-risk 'high', got %q", custom.MinRisk)
	}
	if !custom.Worktree {
		t.Errorf("expected worktree true, got false")
	}
}

func TestRiskScoreToLevel(t *testing.T) {
	tests := []struct {
		score int
		level string
	}{
		{0, "LOW"},
		{25, "LOW"},
		{30, "MEDIUM"},
		{55, "MEDIUM"},
		{60, "HIGH"},
		{75, "HIGH"},
		{80, "CRITICAL"},
		{100, "CRITICAL"},
	}

	for _, tt := range tests {
		got := riskScoreToLevel(tt.score)
		if got != tt.level {
			t.Errorf("score %d: expected %q, got %q", tt.score, tt.level, got)
		}
	}
}

func TestCalculateSymbolRiskScore(t *testing.T) {
	// Added symbol with no callers
	changeAdded := sem.EntityChange{Type: "added", Name: "NewFunc"}
	scoreAdded, _ := calculateSymbolRiskScore(changeAdded, 0, 0, 0, 0, 0, false)
	if scoreAdded >= 30 {
		t.Errorf("added symbol should have low score, got %d", scoreAdded)
	}

	// Deleted symbol with callers (blindspot)
	changeDeleted := sem.EntityChange{Type: "deleted", Name: "DeletedFunc"}
	scoreDeleted, factors := calculateSymbolRiskScore(changeDeleted, 3, 2, 1, 0, 0, false)
	if scoreDeleted < 60 {
		t.Errorf("deleted symbol with callers should have high/critical score, got %d", scoreDeleted)
	}
	foundBlindspot := false
	for _, f := range factors {
		if strings.Contains(f, "UNTESTED BLINDSPOT") {
			foundBlindspot = true
		}
	}
	if !foundBlindspot {
		t.Errorf("expected UNTESTED BLINDSPOT factor in %v", factors)
	}

	// Modified symbol with signature change and test coverage
	changeMod := sem.EntityChange{Type: "modified", Name: "ModFunc"}
	scoreMod, _ := calculateSymbolRiskScore(changeMod, 2, 1, 0, 0, 2, true)
	if scoreMod > 70 {
		t.Errorf("test-covered modification should be mitigated, got %d", scoreMod)
	}
}

func TestRenderChangeRadarOutputs(t *testing.T) {
	rep := ChangeRadarReport{
		FormatVersion:        1,
		RepoRoot:             ".",
		BaseRef:              "main",
		HeadRef:              "HEAD",
		OverallRiskScore:     72,
		OverallRiskLevel:     "HIGH",
		FilesChangedCount:    2,
		SymbolsChangedCount:  3,
		TotalAffectedCallers: 8,
		BlindspotsCount:      1,
		ImpactedSymbols: []SymbolImpact{
			{
				Name:          "Execute",
				Kind:          "function",
				FilePath:      "internal/cli/root.go",
				StartLine:     33,
				ChangeType:    "modified",
				RiskScore:     72,
				RiskLevel:     "HIGH",
				RiskFactors:   []string{"2 direct caller(s) affected", "Signature modified"},
				DirectCallers: []neighborEndpoint{{Name: "main", FilePath: "cmd/entire-graph/main.go", StartLine: 18}},
				CoveringTests: []string{"TestMain"},
			},
		},
		SuggestedTestSuite: []string{"go test ./cmd/entire-graph -run '^TestMain$'"},
		AnalysisDurationMS: 15,
	}

	// 1. Text rendering
	var textBuf bytes.Buffer
	if err := renderChangeRadarText(&textBuf, rep); err != nil {
		t.Fatalf("renderChangeRadarText failed: %v", err)
	}
	textStr := textBuf.String()
	if !strings.Contains(textStr, "CHANGERADAR") || !strings.Contains(textStr, "OVERALL PR RISK: [HIGH]") {
		t.Errorf("text output missing expected strings: %s", textStr)
	}

	// 2. Markdown rendering
	var mdBuf bytes.Buffer
	if err := renderChangeRadarMarkdown(&mdBuf, rep); err != nil {
		t.Fatalf("renderChangeRadarMarkdown failed: %v", err)
	}
	mdStr := mdBuf.String()
	if !strings.Contains(mdStr, "# 🔴 ChangeRadar Intelligence Report") || !strings.Contains(mdStr, "Execute") {
		t.Errorf("markdown output missing expected strings: %s", mdStr)
	}

	// 3. JSON rendering
	var jsonBuf bytes.Buffer
	if err := renderChangeRadarJSON(&jsonBuf, rep); err != nil {
		t.Fatalf("renderChangeRadarJSON failed: %v", err)
	}
	var roundtrip ChangeRadarReport
	if err := json.Unmarshal(jsonBuf.Bytes(), &roundtrip); err != nil {
		t.Fatalf("failed to unmarshal JSON output: %v", err)
	}
	if roundtrip.OverallRiskScore != 72 || roundtrip.OverallRiskLevel != "HIGH" {
		t.Errorf("JSON roundtrip mismatch: %+v", roundtrip)
	}
}
