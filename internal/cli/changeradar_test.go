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

// TestTrack2CurveballIncompleteAnalysis verifies the Noon Curveball requirements:
// 1. Incomplete relationships are not presented as certain.
// 2. Identifies when analysis is partial (reflection, generated code, dynamic dispatch).
// 3. Provides a safe fallback or verification path.
// 4. Distinguishes confirmed structural evidence from heuristic and unverified claims.
// 5. Existing behavior for fully resolved code continues to work.
func TestTrack2CurveballIncompleteAnalysis(t *testing.T) {
	// Case 1: Fully resolved static code
	symStatic := sem.SymbolRecord{
		ID:        "sym-1",
		Name:      "ComputeChecksum",
		FilePath:  "internal/util/crypto.go",
		StartLine: 15,
		Kind:      "function",
	}
	changeStatic := sem.EntityChange{
		Type:            "modified",
		Name:            "ComputeChecksum",
		NewSignature:    "func ComputeChecksum(buf []byte) uint32",
		AfterStartLine:  15,
	}
	incomingCalls := map[string][]radarEdgeRecord{
		"sym-1": {
			{fromID: "caller-1", toID: "sym-1"},
		},
	}
	symbolsByID := map[string]sem.SymbolRecord{
		"sym-1":    symStatic,
		"caller-1": {ID: "caller-1", Name: "SaveFile", FilePath: "internal/io/file.go", StartLine: 40},
	}

	impactStatic := analyzeSingleSymbolImpact(
		changeStatic,
		"internal/util/crypto.go",
		&symStatic,
		symbolsByID,
		incomingCalls,
		nil,
		nil,
		nil,
		2,
		15,
	)

	if impactStatic.EvidenceTier != TierConfirmed {
		t.Errorf("expected TierConfirmed for static code with callers, got %v", impactStatic.EvidenceTier)
	}
	if impactStatic.EvidenceConfidence != "HIGH" {
		t.Errorf("expected HIGH confidence, got %v", impactStatic.EvidenceConfidence)
	}
	if impactStatic.RequiresVerification {
		t.Errorf("fully resolved static code should not require special verification")
	}

	// Case 2: Generated file code
	changeGen := sem.EntityChange{
		Type:           "modified",
		Name:           "GeneratedPayload",
		NewSignature:   "type GeneratedPayload struct",
		AfterStartLine: 10,
	}
	impactGen := analyzeSingleSymbolImpact(
		changeGen,
		"internal/cli/testdata/partial_analysis/api_gen.go",
		nil,
		symbolsByID,
		incomingCalls,
		nil,
		nil,
		nil,
		2,
		15,
	)

	if impactGen.EvidenceTier != TierHeuristic {
		t.Errorf("expected TierHeuristic for generated code, got %v", impactGen.EvidenceTier)
	}
	if !impactGen.RequiresVerification {
		t.Errorf("generated code must require verification")
	}
	if !strings.Contains(impactGen.VerificationAdvice, "Auto-generated code") {
		t.Errorf("expected verification advice for generated code, got: %s", impactGen.VerificationAdvice)
	}

	// Case 3: Dynamic reflection dispatch
	changeDynamic := sem.EntityChange{
		Type:           "modified",
		Name:           "InvokeDynamicHandler",
		NewSignature:   "func (d *DynamicDispatcher) Invoke(name string, args ...interface{}) (interface{}, error)",
		AfterStartLine: 25,
	}
	impactDynamic := analyzeSingleSymbolImpact(
		changeDynamic,
		"internal/cli/testdata/partial_analysis/dynamic_dispatch.go",
		nil,
		symbolsByID,
		incomingCalls,
		nil,
		nil,
		nil,
		2,
		15,
	)

	if impactDynamic.EvidenceTier != TierHeuristic {
		t.Errorf("expected TierHeuristic for dynamic reflection, got %v", impactDynamic.EvidenceTier)
	}
	if !impactDynamic.RequiresVerification {
		t.Errorf("dynamic reflection must require verification")
	}

	// Case 4: Test Report Completeness & Safe Fallback
	diffResult := sem.Result{
		Files: []sem.FileChange{
			{
				Path:    "dynamic_dispatch.go",
				Changes: []sem.EntityChange{changeDynamic},
			},
		},
	}
	snapshot := sem.ProviderSnapshot{
		Symbols: []sem.SymbolRecord{symStatic},
	}
	flags := changeRadarFlags{
		Depth:   2,
		Limit:   15,
		Format:  "text",
		MinRisk: "all",
	}

	reportPartial := buildChangeRadarReport(
		".", "HEAD~1", "HEAD",
		diffResult,
		snapshot,
		flags,
	)

	if !reportPartial.Completeness.IsPartial {
		t.Errorf("report containing dynamic dispatch must report IsPartial = true")
	}
	if reportPartial.Completeness.Status != "PARTIAL" {
		t.Errorf("expected status 'PARTIAL', got %q", reportPartial.Completeness.Status)
	}
	if len(reportPartial.Completeness.SafeFallbackCommands) == 0 {
		t.Errorf("expected safe fallback commands when analysis is partial")
	}

	// Verify safe fallback command includes race detector / full package test
	hasRaceTest := false
	for _, cmd := range reportPartial.Completeness.SafeFallbackCommands {
		if strings.Contains(cmd, "go test") && strings.Contains(cmd, "-race") {
			hasRaceTest = true
		}
	}
	if !hasRaceTest {
		t.Errorf("expected go test -race in fallback commands, got: %v", reportPartial.Completeness.SafeFallbackCommands)
	}

	// Case 5: Verify text rendering of partial analysis includes warning & fallback
	var textBuf bytes.Buffer
	if err := renderChangeRadarText(&textBuf, reportPartial); err != nil {
		t.Fatalf("renderChangeRadarText failed: %v", err)
	}
	output := textBuf.String()
	if !strings.Contains(output, "ANALYSIS CERTAINTY: [PARTIAL]") {
		t.Errorf("text output missing partial certainty banner: %s", output)
	}
	if !strings.Contains(output, "Graph is evidence, not an oracle") {
		t.Errorf("text output missing curveball principle: %s", output)
	}
	if !strings.Contains(output, "Safe Fallback Verification Commands:") {
		t.Errorf("text output missing safe fallback section: %s", output)
	}
}
