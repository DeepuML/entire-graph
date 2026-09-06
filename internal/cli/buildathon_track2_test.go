package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entireio/entire-graph/internal/sem"
)

// TestBuildathonTrack2ComprehensiveSuite verifies all Track 2 (Graph Intelligence)
// requirements and the Noon Curveball ("Graph is Evidence, Not an Oracle") constraints.
func TestBuildathonTrack2ComprehensiveSuite(t *testing.T) {
	t.Run("1. Core Track 2: Semantic Risk Scoring and Blast Radius Calculation", func(t *testing.T) {
		// Verify risk level thresholds
		if riskScoreToLevel(10) != "LOW" {
			t.Errorf("expected LOW for score 10")
		}
		if riskScoreToLevel(45) != "MEDIUM" {
			t.Errorf("expected MEDIUM for score 45")
		}
		if riskScoreToLevel(65) != "HIGH" {
			t.Errorf("expected HIGH for score 65")
		}
		if riskScoreToLevel(85) != "CRITICAL" {
			t.Errorf("expected CRITICAL for score 85")
		}

		// Verify deletion of symbol with multiple callers triggers CRITICAL risk
		delChange := sem.EntityChange{
			Type: "deleted",
			Name: "CoreRouter",
		}
		delScore, factors := calculateSymbolRiskScore(delChange, 5, 12, 4, 2, 0, false)
		if delScore < 80 {
			t.Errorf("expected score >= 80 for deleted core router with callers, got %d", delScore)
		}
		hasBlindspot := false
		for _, f := range factors {
			if strings.Contains(f, "UNTESTED BLINDSPOT") {
				hasBlindspot = true
			}
		}
		if !hasBlindspot {
			t.Errorf("expected untested blindspot factor when 0 tests exist")
		}

		// Verify test coverage mitigation
		modChange := sem.EntityChange{
			Type: "modified",
			Name: "DataParser",
		}
		modScoreWithTests, _ := calculateSymbolRiskScore(modChange, 2, 1, 1, 0, 3, false)
		modScoreNoTests, _ := calculateSymbolRiskScore(modChange, 2, 1, 1, 0, 0, false)
		if modScoreWithTests >= modScoreNoTests {
			t.Errorf("expected test coverage to mitigate risk score: withTests=%d, noTests=%d", modScoreWithTests, modScoreNoTests)
		}
	})

	t.Run("2. Track 2 Smart Test Selection & Command Synthesis", func(t *testing.T) {
		// Go file with tests
		cmd1 := deriveVerifyCommand("internal/cli/changeradar.go", []string{"TestParseFlags", "TestRender"})
		if cmd1 != "go test ./internal/cli -run '^TestParseFlags$'" {
			t.Errorf("unexpected go test command: %s", cmd1)
		}

		// Go file without tests
		cmd2 := deriveVerifyCommand("internal/cli/util.go", nil)
		if cmd2 != "go test ./internal/cli" {
			t.Errorf("unexpected fallback test command: %s", cmd2)
		}

		// Python test command
		cmd3 := deriveVerifyCommand("services/api/routes.py", []string{"test_routes"})
		if cmd3 != "pytest services/api" {
			t.Errorf("unexpected python test command: %s", cmd3)
		}
	})

	t.Run("3. Noon Curveball: Confirmed Structural Evidence vs Incomplete Static Analysis", func(t *testing.T) {
		symAST := sem.SymbolRecord{
			ID:        "sym-clean",
			Name:      "StandardStaticFunc",
			FilePath:  "internal/service/clean.go",
			StartLine: 10,
			Kind:      "function",
		}
		callerAST := sem.SymbolRecord{
			ID:        "caller-clean",
			Name:      "ExecuteStatic",
			FilePath:  "cmd/app/main.go",
			StartLine: 20,
			Kind:      "function",
		}
		symbolsByID := map[string]sem.SymbolRecord{
			"sym-clean":    symAST,
			"caller-clean": callerAST,
		}
		incomingCalls := map[string][]radarEdgeRecord{
			"sym-clean": {{fromID: "caller-clean", toID: "sym-clean"}},
		}

		changeClean := sem.EntityChange{
			Type:           "modified",
			Name:           "StandardStaticFunc",
			NewSignature:   "func StandardStaticFunc(x int) int",
			AfterStartLine: 10,
		}

		impactClean := analyzeSingleSymbolImpact(
			changeClean,
			"internal/service/clean.go",
			&symAST,
			symbolsByID,
			incomingCalls,
			nil, nil, nil, 2, 15,
		)

		if impactClean.EvidenceTier != TierConfirmed {
			t.Errorf("expected TierConfirmed for standard static code, got %s", impactClean.EvidenceTier)
		}
		if impactClean.EvidenceConfidence != "HIGH" {
			t.Errorf("expected HIGH confidence, got %s", impactClean.EvidenceConfidence)
		}
		if impactClean.RequiresVerification {
			t.Errorf("standard static code should not require special verification")
		}
	})

	t.Run("4. Noon Curveball: Dynamic Reflection & Runtime Dispatch Fixture Detection", func(t *testing.T) {
		// Read actual fixture file from disk to ensure fixture exists and is valid
		fixturePath := filepath.Join("testdata", "partial_analysis", "dynamic_dispatch.go")
		content, err := os.ReadFile(fixturePath)
		if err != nil {
			t.Fatalf("dynamic dispatch fixture missing at %s: %v", fixturePath, err)
		}
		if !strings.Contains(string(content), "reflect.ValueOf") {
			t.Fatalf("fixture must demonstrate reflection")
		}

		// Analyze dynamic reflection symbol
		changeDynamic := sem.EntityChange{
			Type:           "modified",
			Name:           "Invoke",
			NewSignature:   "func (d *DynamicDispatcher) Invoke(name string, args ...interface{}) (interface{}, error)",
			AfterStartLine: 23,
		}
		impactDynamic := analyzeSingleSymbolImpact(
			changeDynamic,
			filepath.ToSlash(fixturePath),
			nil,
			nil, nil, nil, nil, nil, 2, 15,
		)

		if impactDynamic.EvidenceTier != TierHeuristic {
			t.Errorf("expected TierHeuristic for dynamic reflection, got %s", impactDynamic.EvidenceTier)
		}
		if impactDynamic.EvidenceConfidence != "MEDIUM" {
			t.Errorf("expected MEDIUM confidence for dynamic reflection, got %s", impactDynamic.EvidenceConfidence)
		}
		if !impactDynamic.RequiresVerification {
			t.Errorf("dynamic reflection must flag RequiresVerification = true")
		}
		if !strings.Contains(impactDynamic.VerificationAdvice, "Dynamic dispatch / reflection") {
			t.Errorf("missing dynamic dispatch warning advice: %s", impactDynamic.VerificationAdvice)
		}
	})

	t.Run("5. Noon Curveball: Generated Code Fixture Detection", func(t *testing.T) {
		genFixturePath := filepath.Join("testdata", "partial_analysis", "api_gen.go")
		if _, err := os.Stat(genFixturePath); os.IsNotExist(err) {
			t.Fatalf("generated code fixture missing at %s", genFixturePath)
		}

		changeGen := sem.EntityChange{
			Type:           "modified",
			Name:           "GeneratedPayload",
			NewSignature:   "type GeneratedPayload struct",
			AfterStartLine: 6,
		}
		impactGen := analyzeSingleSymbolImpact(
			changeGen,
			filepath.ToSlash(genFixturePath),
			nil,
			nil, nil, nil, nil, nil, 2, 15,
		)

		if impactGen.EvidenceTier != TierHeuristic {
			t.Errorf("expected TierHeuristic for generated code, got %s", impactGen.EvidenceTier)
		}
		if !impactGen.RequiresVerification {
			t.Errorf("generated code must flag RequiresVerification = true")
		}
	})

	t.Run("6. Noon Curveball: Unverified Claims & Missing Static Callers", func(t *testing.T) {
		changeUncalled := sem.EntityChange{
			Type:           "modified",
			Name:           "ExportedPublicAPINoCallers",
			NewSignature:   "func ExportedPublicAPINoCallers()",
			AfterStartLine: 50,
		}
		impactUncalled := analyzeSingleSymbolImpact(
			changeUncalled,
			"internal/api/handler.go",
			nil,
			nil, nil, nil, nil, nil, 2, 15,
		)

		if impactUncalled.EvidenceTier != TierUnverified {
			t.Errorf("expected TierUnverified for symbol with 0 static callers, got %s", impactUncalled.EvidenceTier)
		}
		if impactUncalled.EvidenceConfidence != "LOW" {
			t.Errorf("expected LOW confidence, got %s", impactUncalled.EvidenceConfidence)
		}
		if !impactUncalled.RequiresVerification {
			t.Errorf("unverified symbol must flag RequiresVerification = true")
		}
	})

	t.Run("7. Completeness Engine & Safe Fallback Verification Suite", func(t *testing.T) {
		diffPartial := sem.Result{
			Files: []sem.FileChange{
				{
					Path: "internal/cli/testdata/partial_analysis/dynamic_dispatch.go",
					Changes: []sem.EntityChange{
						{
							Type:           "modified",
							Name:           "Invoke",
							NewSignature:   "func (d *DynamicDispatcher) Invoke(name string, args ...interface{}) (interface{}, error)",
							AfterStartLine: 23,
						},
					},
				},
			},
		}
		flags := changeRadarFlags{Depth: 2, Limit: 15, Format: "text", MinRisk: "all"}

		report := buildChangeRadarReport(".", "HEAD~1", "HEAD", diffPartial, sem.ProviderSnapshot{}, flags)

		if report.Completeness.Status != "PARTIAL" {
			t.Errorf("expected report status 'PARTIAL', got %s", report.Completeness.Status)
		}
		if !report.Completeness.IsPartial {
			t.Errorf("expected IsPartial = true")
		}
		if report.Completeness.ConfidenceScore >= 1.0 || report.Completeness.ConfidenceScore < 0.5 {
			t.Errorf("expected degraded confidence score between 0.5 and 0.95, got %f", report.Completeness.ConfidenceScore)
		}
		if len(report.Completeness.PartialReasons) == 0 {
			t.Errorf("expected itemized partial reasons")
		}

		// Safe Fallback commands must be present
		if len(report.Completeness.SafeFallbackCommands) == 0 {
			t.Fatalf("safe fallback verification commands missing")
		}
		foundRaceTest := false
		for _, cmd := range report.Completeness.SafeFallbackCommands {
			if strings.Contains(cmd, "go test") && strings.Contains(cmd, "-race") {
				foundRaceTest = true
			}
		}
		if !foundRaceTest {
			t.Errorf("expected go test with -race detector in fallback commands: %v", report.Completeness.SafeFallbackCommands)
		}
	})

	t.Run("8. Multi-Format Output Integrity (Text, Markdown, JSON)", func(t *testing.T) {
		rep := ChangeRadarReport{
			FormatVersion:    1,
			RepoRoot:         "github.com/entireio/entire-graph",
			BaseRef:          "origin/main",
			HeadRef:          "HEAD",
			OverallRiskScore: 78,
			OverallRiskLevel: "HIGH",
			Completeness: AnalysisCompleteness{
				Status:                 "PARTIAL",
				IsPartial:              true,
				ConfidenceScore:        0.75,
				PartialReasons:         []string{"Dynamic reflection detected in dynamic_dispatch.go"},
				SafeFallbackCommands:   []string{"go test -v -race ./..."},
				VerificationGuidelines: []string{"Graph is evidence, not an oracle"},
			},
			FilesChangedCount:    1,
			SymbolsChangedCount:  1,
			TotalAffectedCallers: 3,
			BlindspotsCount:      1,
			ImpactedSymbols: []SymbolImpact{
				{
					Name:                 "Invoke",
					Kind:                 "function",
					FilePath:             "internal/cli/testdata/partial_analysis/dynamic_dispatch.go",
					StartLine:            23,
					ChangeType:           "modified",
					RiskScore:            78,
					RiskLevel:            "HIGH",
					EvidenceTier:         TierHeuristic,
					EvidenceConfidence:   "MEDIUM",
					RequiresVerification: true,
					VerificationAdvice:   "Check dynamic reflection handler map",
					RiskFactors:          []string{"Entity modified", "⚠️ Heuristic evidence: Dynamic dispatch / reflection"},
				},
			},
			SuggestedTestSuite: []string{"go test -v -race ./..."},
		}

		// Text output verification
		var textBuf bytes.Buffer
		if err := renderChangeRadarText(&textBuf, rep); err != nil {
			t.Fatalf("renderChangeRadarText failed: %v", err)
		}
		textOut := textBuf.String()
		if !strings.Contains(textOut, "ANALYSIS CERTAINTY: [PARTIAL]") {
			t.Errorf("text missing partial certainty banner")
		}
		if !strings.Contains(textOut, "Graph is evidence, not an oracle") {
			t.Errorf("text missing curveball maxim")
		}
		if !strings.Contains(textOut, "[HEURISTIC]") {
			t.Errorf("text missing heuristic tier badge")
		}
		if !strings.Contains(textOut, "Safe Fallback Verification Commands:") {
			t.Errorf("text missing fallback command section")
		}

		// Markdown output verification
		var mdBuf bytes.Buffer
		if err := renderChangeRadarMarkdown(&mdBuf, rep); err != nil {
			t.Fatalf("renderChangeRadarMarkdown failed: %v", err)
		}
		mdOut := mdBuf.String()
		if !strings.Contains(mdOut, "# 🔴 ChangeRadar Intelligence Report") {
			t.Errorf("markdown missing header")
		}
		if !strings.Contains(mdOut, "Analysis Certainty: PARTIAL") {
			t.Errorf("markdown missing certainty block")
		}
		if !strings.Contains(mdOut, "Evidence Tier") {
			t.Errorf("markdown table missing Evidence Tier column")
		}
		if !strings.Contains(mdOut, "Heuristic") {
			t.Errorf("markdown table missing Heuristic label")
		}

		// JSON serialization and deserialization roundtrip
		var jsonBuf bytes.Buffer
		if err := renderChangeRadarJSON(&jsonBuf, rep); err != nil {
			t.Fatalf("renderChangeRadarJSON failed: %v", err)
		}
		var parsed ChangeRadarReport
		if err := json.Unmarshal(jsonBuf.Bytes(), &parsed); err != nil {
			t.Fatalf("JSON roundtrip failed: %v", err)
		}
		if parsed.Completeness.Status != "PARTIAL" || parsed.Completeness.ConfidenceScore != 0.75 {
			t.Errorf("JSON roundtrip completeness mismatch: %+v", parsed.Completeness)
		}
		if len(parsed.ImpactedSymbols) != 1 || parsed.ImpactedSymbols[0].EvidenceTier != TierHeuristic {
			t.Errorf("JSON roundtrip symbol tier mismatch: %+v", parsed.ImpactedSymbols)
		}
	})
}
