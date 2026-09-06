package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entireio/entire-graph/internal/gitutil"
	"github.com/entireio/entire-graph/internal/sem"
	"github.com/entireio/entire-graph/internal/termsafe"
)

// changeRadarFlags holds CLI flags for the changeradar command.
type changeRadarFlags struct {
	Repo         string
	Base         string
	Head         string
	Worktree     bool
	Format       string // text, markdown, json
	Profile      string // full, fast, syntax-only
	Depth        int
	Limit        int
	CacheDir     string
	DisableCache bool
	MinRisk      string // all, low, medium, high, critical
}

type radarEdgeRecord struct {
	fromID   string
	toID     string
	relType  string
	evidence []sem.Evidence
}

// EvidenceTier categorizes the certainty of graph relationships.
// Follows Track 2 Curveball principle: "Graph is evidence, not an oracle".
type EvidenceTier string

const (
	TierConfirmed  EvidenceTier = "CONFIRMED_STRUCTURAL" // Exact static AST call graph match
	TierHeuristic  EvidenceTier = "HEURISTIC"            // Inferred via dynamic dispatch, reflection, interface duck-typing, or generated code
	TierUnverified EvidenceTier = "UNVERIFIED_CLAIM"     // Needs source/test verification (0 static callers found or dynamic target)
)

// AnalysisCompleteness captures whether static analysis is complete or degraded/partial.
type AnalysisCompleteness struct {
	Status                 string   `json:"status"` // "COMPLETE" or "PARTIAL"
	IsPartial              bool     `json:"is_partial"`
	ConfidenceScore        float64  `json:"confidence_score"` // 0.0 to 1.0
	PartialReasons         []string `json:"partial_reasons,omitempty"`
	SafeFallbackCommands   []string `json:"safe_fallback_commands,omitempty"`
	VerificationGuidelines []string `json:"verification_guidelines,omitempty"`
}

// SymbolImpact records the blast-radius impact analysis for one changed symbol.
type SymbolImpact struct {
	Name                 string             `json:"name"`
	Kind                 string             `json:"kind"`
	FilePath             string             `json:"file_path"`
	StartLine            int                `json:"start_line"`
	ChangeType           string             `json:"change_type"` // added, modified, deleted
	OldSignature         string             `json:"old_signature,omitempty"`
	NewSignature         string             `json:"new_signature,omitempty"`
	SignatureDiff        bool               `json:"signature_diff"`
	RiskScore            int                `json:"risk_score"`
	RiskLevel            string             `json:"risk_level"` // LOW, MEDIUM, HIGH, CRITICAL
	RiskFactors          []string           `json:"risk_factors"`
	EvidenceTier         EvidenceTier       `json:"evidence_tier"`
	EvidenceConfidence   string             `json:"evidence_confidence"` // HIGH, MEDIUM, LOW
	RequiresVerification bool               `json:"requires_verification"`
	VerificationAdvice   string             `json:"verification_advice,omitempty"`
	DirectCallers        []neighborEndpoint `json:"direct_callers"`
	Transitive           []neighborEndpoint `json:"transitive_callers"`
	TypeConsumers        []neighborEndpoint `json:"type_consumers"`
	Routes               []string           `json:"routes"`
	CoveringTests        []string           `json:"covering_tests"`
	SuggestedVerify      string             `json:"suggested_verify,omitempty"`
	IsBlindspot          bool               `json:"is_blindspot"` // callers exist but 0 tests
}

// ChangeRadarReport is the top-level report returned by changeradar.
type ChangeRadarReport struct {
	FormatVersion        int                  `json:"format_version"`
	RepoRoot             string               `json:"repo_root"`
	BaseRef              string               `json:"base_ref"`
	HeadRef              string               `json:"head_ref"`
	OverallRiskScore     int                  `json:"overall_risk_score"`
	OverallRiskLevel     string               `json:"overall_risk_level"`
	Completeness         AnalysisCompleteness `json:"completeness"`
	FilesChangedCount    int                  `json:"files_changed_count"`
	SymbolsChangedCount  int                  `json:"symbols_changed_count"`
	TotalAffectedCallers int                  `json:"total_affected_callers"`
	BlindspotsCount      int                  `json:"blindspots_count"`
	ImpactedSymbols      []SymbolImpact       `json:"impacted_symbols"`
	SuggestedTestSuite   []string             `json:"suggested_test_suite"`
	AnalysisDurationMS   int64                `json:"analysis_duration_ms"`
}

func parseChangeRadarFlags(args []string) (changeRadarFlags, error) {
	flags := changeRadarFlags{
		Repo:    ".",
		Format:  "text",
		Profile: "full",
		Depth:   2,
		Limit:   15,
		MinRisk: "all",
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--repo":
			if i+1 >= len(args) {
				return flags, errors.New("--repo requires a directory path")
			}
			i++
			flags.Repo = args[i]
		case strings.HasPrefix(arg, "--repo="):
			flags.Repo = strings.TrimPrefix(arg, "--repo=")
		case arg == "--base":
			if i+1 >= len(args) {
				return flags, errors.New("--base requires a git ref")
			}
			i++
			flags.Base = args[i]
		case strings.HasPrefix(arg, "--base="):
			flags.Base = strings.TrimPrefix(arg, "--base=")
		case arg == "--head":
			if i+1 >= len(args) {
				return flags, errors.New("--head requires a git ref")
			}
			i++
			flags.Head = args[i]
		case strings.HasPrefix(arg, "--head="):
			flags.Head = strings.TrimPrefix(arg, "--head=")
		case arg == "--worktree":
			flags.Worktree = true
		case arg == "--format":
			if i+1 >= len(args) {
				return flags, errors.New("--format requires text, markdown, or json")
			}
			i++
			flags.Format = strings.ToLower(args[i])
		case strings.HasPrefix(arg, "--format="):
			flags.Format = strings.ToLower(strings.TrimPrefix(arg, "--format="))
		case arg == "--profile":
			if i+1 >= len(args) {
				return flags, errors.New("--profile requires full, fast, or syntax-only")
			}
			i++
			flags.Profile = args[i]
		case strings.HasPrefix(arg, "--profile="):
			flags.Profile = strings.TrimPrefix(arg, "--profile=")
		case arg == "--depth":
			if i+1 >= len(args) {
				return flags, errors.New("--depth requires an integer")
			}
			i++
			var d int
			if _, err := fmt.Sscanf(args[i], "%d", &d); err == nil && d > 0 {
				flags.Depth = d
			}
		case strings.HasPrefix(arg, "--depth="):
			var d int
			if _, err := fmt.Sscanf(strings.TrimPrefix(arg, "--depth="), "%d", &d); err == nil && d > 0 {
				flags.Depth = d
			}
		case arg == "--min-risk":
			if i+1 >= len(args) {
				return flags, errors.New("--min-risk requires all, low, medium, high, or critical")
			}
			i++
			flags.MinRisk = strings.ToLower(args[i])
		case strings.HasPrefix(arg, "--min-risk="):
			flags.MinRisk = strings.ToLower(strings.TrimPrefix(arg, "--min-risk="))
		case arg == "--cache-dir":
			if i+1 >= len(args) {
				return flags, errors.New("--cache-dir requires a path")
			}
			i++
			flags.CacheDir = args[i]
		case strings.HasPrefix(arg, "--cache-dir="):
			flags.CacheDir = strings.TrimPrefix(arg, "--cache-dir=")
		case arg == "--disable-cache":
			flags.DisableCache = true
		}
	}

	if flags.Format != "text" && flags.Format != "markdown" && flags.Format != "json" {
		flags.Format = "text"
	}
	return flags, nil
}

func runChangeRadar(ctx context.Context, opts Options, args []string) error {
	flags, err := parseChangeRadarFlags(args)
	if err != nil {
		return err
	}

	repo, err := resolveRepo(ctx, opts.Env, flags.Repo)
	if err != nil {
		return err
	}

	profile, err := parseProfile(flags.Profile)
	if err != nil {
		return err
	}

	baseRef, headRef, err := resolveDefaultBaseAndHead(ctx, repo, flags.Base, flags.Head)
	if err != nil {
		return err
	}

	start := time.Now()

	// 1. Analyze Git Diff changes
	diffResult, err := sem.AnalyzeGitRangeWithOptions(ctx, repo, baseRef, headRef, nil, sem.AnalyzeOptions{})
	if err != nil {
		return fmt.Errorf("failed to analyze git diff between %s and %s: %w", baseRef, headRef, err)
	}

	// 2. Load or Build Semantic Graph Snapshot
	cacheDir := resolveCacheDir(flags.CacheDir, opts.Env.PluginDataDir)
	snapshot, _, err := sem.LoadOrBuildProviderSnapshot(ctx, repo, opts.Version, sem.ProviderSnapshotOptions{
		NoNetwork: true,
		Worktree:  flags.Worktree,
		Profile:   profile,
	}, cacheDir, flags.DisableCache)
	if err != nil {
		return fmt.Errorf("failed to load graph snapshot: %w", err)
	}

	// 3. Build ChangeRadar Report
	report := buildChangeRadarReport(repo, baseRef, headRef, diffResult, snapshot, flags)
	report.AnalysisDurationMS = time.Since(start).Milliseconds()

	// 4. Render output
	switch flags.Format {
	case "json":
		return renderChangeRadarJSON(opts.Stdout, report)
	case "markdown":
		return renderChangeRadarMarkdown(opts.Stdout, report)
	default:
		return renderChangeRadarText(opts.Stdout, report)
	}
}

func resolveDefaultBaseAndHead(ctx context.Context, repo, explicitBase, explicitHead string) (string, string, error) {
	head := explicitHead
	if head == "" {
		head = "HEAD"
	}

	base := explicitBase
	if base == "" {
		candidates := []string{"origin/main", "main", "origin/master", "master"}
		for _, cand := range candidates {
			candOID, errCand := gitutil.RevParse(ctx, repo, cand)
			headOID, errHead := gitutil.RevParse(ctx, repo, head)
			if errCand == nil && errHead == nil && candOID != headOID {
				base = cand
				break
			}
		}
		if base == "" {
			if _, err := gitutil.RevParse(ctx, repo, "HEAD~1"); err == nil {
				base = "HEAD~1"
			} else {
				base = head
			}
		}
	}
	return base, head, nil
}

func buildChangeRadarReport(
	repoRoot, baseRef, headRef string,
	diffResult sem.Result,
	snapshot sem.ProviderSnapshot,
	flags changeRadarFlags,
) ChangeRadarReport {
	// Index snapshot symbols
	symbolsByID := make(map[string]sem.SymbolRecord, len(snapshot.Symbols))
	symbolsByPathAndName := make(map[string][]sem.SymbolRecord)
	symbolsByName := make(map[string][]sem.SymbolRecord)
	for _, sym := range snapshot.Symbols {
		symbolsByID[sym.ID] = sym
		cleanPath := filepath.ToSlash(sym.FilePath)
		key := cleanPath + ":" + sym.Name
		symbolsByPathAndName[key] = append(symbolsByPathAndName[key], sym)
		symbolsByName[sym.Name] = append(symbolsByName[sym.Name], sym)
	}

	// Build graph relation indices
	incomingCalls := make(map[string][]radarEdgeRecord)
	incomingTypes := make(map[string][]radarEdgeRecord)
	incomingTests := make(map[string][]radarEdgeRecord)
	routeHandlers := make(map[string]bool)

	for _, rel := range snapshot.Relations {
		rec := radarEdgeRecord{
			fromID:   rel.FromID,
			toID:     rel.ToID,
			relType:  rel.Type,
			evidence: rel.Evidence,
		}
		switch rel.Type {
		case "CALLS", "ASYNC_CALLS", "CONSTRUCTS":
			incomingCalls[rel.ToID] = append(incomingCalls[rel.ToID], rec)
		case "USES_TYPE", "PARAM_TYPE", "RETURNS_TYPE":
			incomingTypes[rel.ToID] = append(incomingTypes[rel.ToID], rec)
		case "TESTS":
			incomingTests[rel.ToID] = append(incomingTests[rel.ToID], rec)
		case "HANDLES_ROUTE", "HANDLES_GRPC", "HANDLES_GRAPHQL", "HANDLES_TRPC", "HTTP_CALLS":
			routeHandlers[rel.FromID] = true
			routeHandlers[rel.ToID] = true
		}
	}

	var impacts []SymbolImpact
	totalCallersCount := 0
	blindspotsCount := 0
	suggestedTestsSet := make(map[string]bool)

	for _, file := range diffResult.Files {
		cleanFilePath := filepath.ToSlash(file.Path)
		for _, change := range file.Changes {
			// Locate matching SymbolRecord in snapshot
			var matchedSymbol *sem.SymbolRecord
			candidates := symbolsByPathAndName[cleanFilePath+":"+change.Name]
			if len(candidates) > 0 {
				matchedSymbol = &candidates[0]
			} else if len(symbolsByName[change.Name]) > 0 {
				matchedSymbol = &symbolsByName[change.Name][0]
			}

			impact := analyzeSingleSymbolImpact(
				change,
				cleanFilePath,
				matchedSymbol,
				symbolsByID,
				incomingCalls,
				incomingTypes,
				incomingTests,
				routeHandlers,
				flags.Depth,
				flags.Limit,
			)

			if shouldIncludeRisk(impact.RiskLevel, flags.MinRisk) {
				impacts = append(impacts, impact)
				totalCallersCount += len(impact.DirectCallers) + len(impact.Transitive)
				if impact.IsBlindspot {
					blindspotsCount++
				}
				if impact.SuggestedVerify != "" {
					suggestedTestsSet[impact.SuggestedVerify] = true
				}
			}
		}
	}

	// Sort impacts by RiskScore descending
	sort.Slice(impacts, func(i, j int) bool {
		return impacts[i].RiskScore > impacts[j].RiskScore
	})

	var suggestedSuite []string
	for testCmd := range suggestedTestsSet {
		suggestedSuite = append(suggestedSuite, testCmd)
	}
	sort.Strings(suggestedSuite)
	if len(suggestedSuite) > 10 {
		suggestedSuite = suggestedSuite[:10]
	}

	// Calculate overall PR risk score
	overallScore := 0
	if len(impacts) > 0 {
		highest := impacts[0].RiskScore
		avg := 0
		for _, imp := range impacts {
			avg += imp.RiskScore
		}
		avg = avg / len(impacts)
		overallScore = (highest*7 + avg*3) / 10
		if blindspotsCount > 0 && overallScore < 95 {
			overallScore += 5
		}
		if overallScore > 100 {
			overallScore = 100
		}
	}

	// Compute Completeness & Evidence Tiering (Track 2: Graph is evidence, not an oracle)
	completeness := AnalysisCompleteness{
		Status:          "COMPLETE",
		ConfidenceScore: 1.0,
	}

	var partialReasons []string
	heuristicCount := 0
	unverifiedCount := 0

	for _, imp := range impacts {
		switch imp.EvidenceTier {
		case TierHeuristic:
			heuristicCount++
		case TierUnverified:
			unverifiedCount++
		}
	}

	if heuristicCount > 0 {
		partialReasons = append(partialReasons, fmt.Sprintf("%d symbol(s) involve dynamic dispatch, reflection, or generated code (static graph is incomplete)", heuristicCount))
	}
	if unverifiedCount > 0 {
		partialReasons = append(partialReasons, fmt.Sprintf("%d symbol(s) have unverified static claims (0 callers found in AST)", unverifiedCount))
	}

	if len(partialReasons) > 0 {
		completeness.Status = "PARTIAL"
		completeness.IsPartial = true
		total := len(impacts)
		if total == 0 {
			total = 1
		}
		ratio := float64(total-heuristicCount-unverifiedCount) / float64(total)
		if ratio < 0.3 {
			completeness.ConfidenceScore = 0.50
		} else {
			completeness.ConfidenceScore = 0.50 + (ratio * 0.45)
		}
		completeness.PartialReasons = partialReasons
		completeness.SafeFallbackCommands = []string{
			"go test -v -race ./...",
			"go vet ./...",
		}
		completeness.VerificationGuidelines = []string{
			"Graph relationships are structural evidence, not an absolute oracle for dynamic dispatch.",
			"Check reflection, dynamic RPC/HTTP registries, and interface method assertions manually.",
			"Run package-wide tests with race detector enabled to verify dynamic runtime paths.",
		}
	}

	return ChangeRadarReport{
		FormatVersion:        1,
		RepoRoot:             repoRoot,
		BaseRef:              baseRef,
		HeadRef:              headRef,
		OverallRiskScore:     overallScore,
		OverallRiskLevel:     riskScoreToLevel(overallScore),
		Completeness:         completeness,
		FilesChangedCount:    len(diffResult.Files),
		SymbolsChangedCount:  len(impacts),
		TotalAffectedCallers: totalCallersCount,
		BlindspotsCount:      blindspotsCount,
		ImpactedSymbols:      impacts,
		SuggestedTestSuite:   suggestedSuite,
	}
}

func analyzeSingleSymbolImpact(
	change sem.EntityChange,
	cleanFilePath string,
	sym *sem.SymbolRecord,
	symbolsByID map[string]sem.SymbolRecord,
	incomingCalls map[string][]radarEdgeRecord,
	incomingTypes map[string][]radarEdgeRecord,
	incomingTests map[string][]radarEdgeRecord,
	routeHandlers map[string]bool,
	maxDepth, limit int,
) SymbolImpact {
	impact := SymbolImpact{
		Name:          change.Name,
		Kind:          change.Kind,
		FilePath:      cleanFilePath,
		StartLine:     change.AfterStartLine,
		ChangeType:    change.Type,
		OldSignature:  change.OldSignature,
		NewSignature:  change.NewSignature,
		SignatureDiff: change.OldSignature != "" && change.NewSignature != "" && change.OldSignature != change.NewSignature,
	}
	if impact.StartLine == 0 {
		impact.StartLine = change.BeforeStartLine
	}

	// 1. Collect Direct Callers
	seenCallers := make(map[string]bool)
	var directCallers []neighborEndpoint
	var transitiveCallers []neighborEndpoint
	var typeConsumers []neighborEndpoint
	var routesList []string
	coveringTestsMap := make(map[string]bool)
	crossFileCallers := 0

	if sym != nil {
		impact.Kind = sym.Kind
		seenCallers[sym.ID] = true

		// Direct callers (depth 1)
		for _, edge := range incomingCalls[sym.ID] {
			if caller, exists := symbolsByID[edge.fromID]; exists {
				if !seenCallers[caller.ID] {
					seenCallers[caller.ID] = true
					ep := endpointForSymbol(caller)
					directCallers = append(directCallers, ep)
					if filepath.ToSlash(caller.FilePath) != cleanFilePath {
						crossFileCallers++
					}
					if isTestSymbol(caller) {
						coveringTestsMap[caller.Name] = true
					}
					if routeHandlers[caller.ID] || isRouteName(caller.Name) {
						routesList = append(routesList, caller.QualifiedName)
					}
				}
			}
		}

		// Transitive callers (depth 2)
		if maxDepth >= 2 {
			for _, dc := range directCallers {
				for _, edge := range incomingCalls[dc.ID] {
					if tCaller, exists := symbolsByID[edge.fromID]; exists {
						if !seenCallers[tCaller.ID] {
							seenCallers[tCaller.ID] = true
							ep := endpointForSymbol(tCaller)
							transitiveCallers = append(transitiveCallers, ep)
							if isTestSymbol(tCaller) {
								coveringTestsMap[tCaller.Name] = true
							}
						}
					}
				}
			}
		}

		// Type consumers
		for _, edge := range incomingTypes[sym.ID] {
			if consumer, exists := symbolsByID[edge.fromID]; exists {
				typeConsumers = append(typeConsumers, endpointForSymbol(consumer))
			}
		}

		// Tests targeting this symbol
		for _, edge := range incomingTests[sym.ID] {
			if testSym, exists := symbolsByID[edge.fromID]; exists {
				coveringTestsMap[testSym.Name] = true
			}
		}

		if routeHandlers[sym.ID] || isRouteName(sym.Name) {
			routesList = append(routesList, sym.QualifiedName)
		}
	}

	// Look for mirror test file
	mirrorTest := findMirrorTestName(cleanFilePath, change.Name)
	if mirrorTest != "" {
		coveringTestsMap[mirrorTest] = true
	}

	for testName := range coveringTestsMap {
		impact.CoveringTests = append(impact.CoveringTests, testName)
	}
	sort.Strings(impact.CoveringTests)

	impact.DirectCallers = capEndpoints(directCallers, limit)
	impact.Transitive = capEndpoints(transitiveCallers, limit)
	impact.TypeConsumers = capEndpoints(typeConsumers, limit)
	impact.Routes = dedupeStrings(routesList)

	// Suggest Verify Command
	impact.SuggestedVerify = deriveVerifyCommand(cleanFilePath, impact.CoveringTests)

	// Calculate Risk Score & Factors
	impact.RiskScore, impact.RiskFactors = calculateSymbolRiskScore(
		change,
		len(directCallers),
		len(transitiveCallers),
		crossFileCallers,
		len(impact.Routes),
		len(impact.CoveringTests),
		impact.SignatureDiff,
	)
	impact.RiskLevel = riskScoreToLevel(impact.RiskScore)
	impact.IsBlindspot = len(directCallers) > 0 && len(impact.CoveringTests) == 0

	// Evidence Tiering & Completeness Detection (Track 2: Graph is evidence, not an oracle)
	isGenerated := strings.HasSuffix(cleanFilePath, "_gen.go") || strings.HasSuffix(cleanFilePath, ".pb.go") || strings.Contains(cleanFilePath, ".generated.")
	isDynamic := strings.Contains(change.NewSignature, "interface{}") ||
		strings.Contains(change.NewSignature, "any") ||
		strings.Contains(strings.ToLower(change.Name), "reflect") ||
		strings.Contains(strings.ToLower(change.Name), "dynamic") ||
		strings.Contains(strings.ToLower(change.Name), "dispatch")

	if isGenerated {
		impact.EvidenceTier = TierHeuristic
		impact.EvidenceConfidence = "MEDIUM"
		impact.RequiresVerification = true
		impact.VerificationAdvice = "Auto-generated code; verify against source schema rather than relying solely on generated AST."
		impact.RiskFactors = append(impact.RiskFactors, "⚠️ Heuristic evidence: Generated code may mask call relationships")
	} else if isDynamic {
		impact.EvidenceTier = TierHeuristic
		impact.EvidenceConfidence = "MEDIUM"
		impact.RequiresVerification = true
		impact.VerificationAdvice = "Dynamic dispatch / reflection detected; static graph cannot guarantee all runtime invocation call-sites."
		impact.RiskFactors = append(impact.RiskFactors, "⚠️ Heuristic evidence: Dynamic dispatch / reflection patterns detected")
	} else if len(directCallers) == 0 && change.Type != "added" {
		impact.EvidenceTier = TierUnverified
		impact.EvidenceConfidence = "LOW"
		impact.RequiresVerification = true
		impact.VerificationAdvice = "0 static callers found in graph; verify whether this symbol is invoked via reflection, RPC, or HTTP routes."
		impact.RiskFactors = append(impact.RiskFactors, "⚠️ Unverified claim: 0 static callers found in graph (verify runtime callers)")
	} else {
		impact.EvidenceTier = TierConfirmed
		impact.EvidenceConfidence = "HIGH"
		impact.RequiresVerification = false
	}

	return impact
}

func calculateSymbolRiskScore(
	change sem.EntityChange,
	directCallersCount, transitiveCount, crossFileCount, routesCount, testsCount int,
	signatureDiff bool,
) (int, []string) {
	score := 0
	var factors []string

	// Base score by change type
	switch change.Type {
	case "deleted":
		score += 35
		factors = append(factors, "Entity deleted (potential breaking change)")
	case "modified":
		score += 15
		factors = append(factors, "Entity modified")
	default: // added
		score += 5
		factors = append(factors, "New entity added")
	}

	// Signature change
	if signatureDiff {
		score += 20
		factors = append(factors, "Function/type signature altered")
	}

	// Caller blast radius
	if directCallersCount > 0 {
		add := directCallersCount * 6
		if add > 30 {
			add = 30
		}
		score += add
		factors = append(factors, fmt.Sprintf("%d direct caller(s) affected", directCallersCount))
	}

	if transitiveCount > 0 {
		add := transitiveCount * 3
		if add > 15 {
			add = 15
		}
		score += add
		factors = append(factors, fmt.Sprintf("%d transitive caller(s) affected", transitiveCount))
	}

	if crossFileCount > 0 {
		add := crossFileCount * 4
		if add > 16 {
			add = 16
		}
		score += add
		factors = append(factors, fmt.Sprintf("%d cross-file caller(s)", crossFileCount))
	}

	// API route / handler
	if routesCount > 0 {
		score += 20
		factors = append(factors, "Affects external API route / service endpoint")
	}

	// Test coverage mitigation or blindspot penalty
	if testsCount > 0 {
		score -= 15
		factors = append(factors, fmt.Sprintf("Covered by %d test(s) (-15 risk)", testsCount))
	} else if directCallersCount > 0 {
		score += 15
		factors = append(factors, "⚠️ UNTESTED BLINDSPOT: 0 unit tests found for callers")
	}

	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return score, factors
}

func riskScoreToLevel(score int) string {
	switch {
	case score >= 80:
		return "CRITICAL"
	case score >= 60:
		return "HIGH"
	case score >= 30:
		return "MEDIUM"
	default:
		return "LOW"
	}
}

func shouldIncludeRisk(itemLevel, filter string) bool {
	if filter == "" || filter == "all" {
		return true
	}
	order := map[string]int{
		"low":      1,
		"medium":   2,
		"high":     3,
		"critical": 4,
	}
	minVal := order[strings.ToLower(filter)]
	itemVal := order[strings.ToLower(itemLevel)]
	return itemVal >= minVal
}

func isTestSymbol(sym sem.SymbolRecord) bool {
	clean := filepath.ToSlash(sym.FilePath)
	if strings.HasSuffix(clean, "_test.go") || strings.Contains(clean, "/test/") || strings.Contains(clean, "/tests/") {
		return true
	}
	if strings.HasPrefix(sym.Name, "Test") || strings.HasSuffix(sym.Name, "Test") {
		return true
	}
	return false
}

func isRouteName(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "route") || strings.Contains(lower, "handler") ||
		strings.Contains(lower, "endpoint") || strings.Contains(lower, "controller")
}

func findMirrorTestName(filePath, symbolName string) string {
	if strings.Contains(symbolName, "/") || strings.Contains(symbolName, "\\") || strings.Contains(symbolName, ".") || symbolName == "" {
		return ""
	}
	ext := filepath.Ext(filePath)
	base := strings.TrimSuffix(filepath.Base(filePath), ext)
	if ext == ".go" && !strings.HasSuffix(base, "_test") {
		return fmt.Sprintf("Test%s (%s_test.go)", symbolName, base)
	}
	return ""
}

func deriveVerifyCommand(filePath string, tests []string) string {
	ext := filepath.Ext(filePath)
	dir := filepath.ToSlash(filepath.Dir(filePath))

	switch ext {
	case ".go":
		if len(tests) > 0 {
			firstTest := tests[0]
			if idx := strings.Index(firstTest, " "); idx != -1 {
				firstTest = firstTest[:idx]
			}
			if !strings.Contains(firstTest, "/") && !strings.Contains(firstTest, ".") && strings.HasPrefix(firstTest, "Test") {
				return fmt.Sprintf("go test ./%s -run '^%s$'", dir, firstTest)
			}
		}
		return fmt.Sprintf("go test ./%s", dir)
	case ".rs":
		return fmt.Sprintf("cargo test %s", dir)
	case ".py":
		return fmt.Sprintf("pytest %s", dir)
	case ".ts", ".js":
		return fmt.Sprintf("npm test -- %s", dir)
	default:
		return ""
	}
}

func capEndpoints(eps []neighborEndpoint, limit int) []neighborEndpoint {
	if len(eps) > limit && limit > 0 {
		return eps[:limit]
	}
	return eps
}

func dedupeStrings(items []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, it := range items {
		if it != "" && !seen[it] {
			seen[it] = true
			out = append(out, it)
		}
	}
	return out
}

// -------------------------------------------------------------------------
// Renderers: Text, Markdown, JSON
// -------------------------------------------------------------------------

func renderChangeRadarText(out io.Writer, rep ChangeRadarReport) error {
	divider := strings.Repeat("═", 78)
	subdivider := strings.Repeat("─", 78)

	fmt.Fprintln(out, divider)
	fmt.Fprintf(out, "  📡 CHANGERADAR — Pull Request Semantic Risk & Test Intelligence\n")
	fmt.Fprintln(out, divider)
	fmt.Fprintf(out, "Repository: %s\n", rep.RepoRoot)
	fmt.Fprintf(out, "Range:      %s .. %s\n", rep.BaseRef, rep.HeadRef)
	fmt.Fprintf(out, "Analysis:   %dms\n\n", rep.AnalysisDurationMS)

	fmt.Fprintf(out, "OVERALL PR RISK: [%s] (Score: %d/100)\n", rep.OverallRiskLevel, rep.OverallRiskScore)
	fmt.Fprintf(out, "Impact Summary:  %d files changed | %d symbols modified | %d callers affected | %d blindspots\n",
		rep.FilesChangedCount, rep.SymbolsChangedCount, rep.TotalAffectedCallers, rep.BlindspotsCount)
	fmt.Fprintln(out, subdivider)

	// Completeness & Evidence Certainty (Track 2 Curveball)
	if rep.Completeness.IsPartial {
		fmt.Fprintf(out, "⚠️  ANALYSIS CERTAINTY: [PARTIAL] (Confidence: %.0f%% — Graph is evidence, not an oracle)\n", rep.Completeness.ConfidenceScore*100)
		fmt.Fprintln(out, "   Reason(s) for partial static certainty:")
		for _, r := range rep.Completeness.PartialReasons {
			fmt.Fprintf(out, "   • %s\n", r)
		}
		if len(rep.Completeness.SafeFallbackCommands) > 0 {
			fmt.Fprintln(out, "   Safe Fallback Verification Commands:")
			for _, fb := range rep.Completeness.SafeFallbackCommands {
				fmt.Fprintf(out, "     ↳ %s\n", fb)
			}
		}
		fmt.Fprintln(out, subdivider)
	} else {
		fmt.Fprintln(out, "✅ ANALYSIS CERTAINTY: [COMPLETE] (100% Confirmed Structural AST Evidence)")
		fmt.Fprintln(out, subdivider)
	}

	if rep.BlindspotsCount > 0 {
		fmt.Fprintf(out, "⚠️  ATTENTION: %d untested blast-radius blindspot(s) detected!\n", rep.BlindspotsCount)
		fmt.Fprintln(out, "   Existing callers depend on these modified symbols, but no unit tests exercise them.")
		fmt.Fprintln(out, subdivider)
	}

	fmt.Fprintln(out, "CHANGES & BLAST RADIUS BREAKDOWN:")
	maxRender := 15
	renderCount := len(rep.ImpactedSymbols)
	if renderCount > maxRender {
		renderCount = maxRender
	}
	for i := 0; i < renderCount; i++ {
		sym := rep.ImpactedSymbols[i]
		tierLabel := "[CONFIRMED AST]"
		switch sym.EvidenceTier {
		case TierHeuristic:
			tierLabel = "⚠️ [HEURISTIC]"
		case TierUnverified:
			tierLabel = "❓ [REQUIRES VERIFICATION]"
		}

		fmt.Fprintf(out, "\n%d. %s (%s:%d) [%s - %s] %s\n",
			i+1, sym.Name, sym.FilePath, sym.StartLine, sym.Kind, strings.ToUpper(sym.ChangeType), tierLabel)
		fmt.Fprintf(out, "   Risk: %s (%d/100)\n", sym.RiskLevel, sym.RiskScore)
		for _, factor := range sym.RiskFactors {
			fmt.Fprintf(out, "   • %s\n", factor)
		}
		if sym.RequiresVerification && sym.VerificationAdvice != "" {
			fmt.Fprintf(out, "   ↳ Action: %s\n", sym.VerificationAdvice)
		}

		if len(sym.DirectCallers) > 0 {
			fmt.Fprintf(out, "   Direct Callers (%d):\n", len(sym.DirectCallers))
			for _, caller := range sym.DirectCallers {
				fmt.Fprintf(out, "     - %s (%s:%d)\n", caller.Name, caller.FilePath, caller.StartLine)
			}
		}

		if len(sym.Transitive) > 0 {
			fmt.Fprintf(out, "   Transitive Callers (%d):\n", len(sym.Transitive))
			for _, caller := range sym.Transitive {
				fmt.Fprintf(out, "     - %s (%s:%d)\n", caller.Name, caller.FilePath, caller.StartLine)
			}
		}

		if len(sym.Routes) > 0 {
			fmt.Fprintf(out, "   Affected API Routes: %s\n", strings.Join(sym.Routes, ", "))
		}

		if len(sym.CoveringTests) > 0 {
			fmt.Fprintf(out, "   Covering Tests: %s\n", strings.Join(sym.CoveringTests, ", "))
		} else if len(sym.DirectCallers) > 0 {
			fmt.Fprintln(out, "   Covering Tests: NONE (Untested)")
		}

		if sym.SuggestedVerify != "" {
			fmt.Fprintf(out, "   Suggested Test: %s\n", sym.SuggestedVerify)
		}
	}

	if len(rep.ImpactedSymbols) > maxRender {
		fmt.Fprintf(out, "\n... and %d more lower-risk symbols omitted (use --format json to view all)\n", len(rep.ImpactedSymbols)-maxRender)
	}

	if len(rep.SuggestedTestSuite) > 0 {
		fmt.Fprintln(out, "\n"+subdivider)
		fmt.Fprintln(out, "🧪 RECOMMENDED TEST SUITE TO RUN:")
		for idx, testCmd := range rep.SuggestedTestSuite {
			fmt.Fprintf(out, "  [%d] %s\n", idx+1, testCmd)
		}
	}

	fmt.Fprintln(out, divider)
	return nil
}

func renderChangeRadarMarkdown(out io.Writer, rep ChangeRadarReport) error {
	badge := "🟢"
	switch rep.OverallRiskLevel {
	case "CRITICAL":
		badge = "🚨"
	case "HIGH":
		badge = "🔴"
	case "MEDIUM":
		badge = "🟡"
	}

	fmt.Fprintf(out, "# %s ChangeRadar Intelligence Report\n\n", badge)
	fmt.Fprintf(out, "**Overall Risk:** `%s` (Score: **%d/100**) | **Range:** `%s..%s`\n\n",
		rep.OverallRiskLevel, rep.OverallRiskScore, rep.BaseRef, rep.HeadRef)

	if rep.Completeness.IsPartial {
		fmt.Fprintf(out, "> ⚠️ **Analysis Certainty: PARTIAL (%.0f%% Confidence)** — *Graph is evidence, not an oracle.*\n>\n", rep.Completeness.ConfidenceScore*100)
		for _, r := range rep.Completeness.PartialReasons {
			fmt.Fprintf(out, "> - %s\n", r)
		}
		if len(rep.Completeness.SafeFallbackCommands) > 0 {
			fmt.Fprintf(out, ">\n> **Safe Fallback Verification:** `%s`\n\n", strings.Join(rep.Completeness.SafeFallbackCommands, " && "))
		} else {
			fmt.Fprintln(out)
		}
	} else {
		fmt.Fprintln(out, "> ✅ **Analysis Certainty: COMPLETE** — *100% Confirmed Structural AST Evidence.*")
		fmt.Fprintln(out)
	}

	fmt.Fprintln(out, "| Files Changed | Symbols Modified | Callers Affected | Test Blindspots |")
	fmt.Fprintln(out, "| :--- | :--- | :--- | :--- |")
	fmt.Fprintf(out, "| %d | %d | %d | %d |\n\n",
		rep.FilesChangedCount, rep.SymbolsChangedCount, rep.TotalAffectedCallers, rep.BlindspotsCount)

	if rep.BlindspotsCount > 0 {
		fmt.Fprintf(out, "> ⚠️ **Warning:** %d changed symbol(s) have active callers but no covering tests!\n\n", rep.BlindspotsCount)
	}

	fmt.Fprintln(out, "## 💥 Impact & Blast Radius Breakdown")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "| Symbol | Change | Risk | Evidence Tier | Callers | Covering Tests |")
	fmt.Fprintln(out, "| :--- | :--- | :--- | :--- | :--- | :--- |")
	for _, sym := range rep.ImpactedSymbols {
		callersCount := len(sym.DirectCallers) + len(sym.Transitive)
		testsLabel := fmt.Sprintf("%d test(s)", len(sym.CoveringTests))
		if callersCount > 0 && len(sym.CoveringTests) == 0 {
			testsLabel = "**0 (Blindspot!)**"
		}
		tierBadge := "Confirmed AST"
		switch sym.EvidenceTier {
		case TierHeuristic:
			tierBadge = "⚠️ Heuristic"
		case TierUnverified:
			tierBadge = "❓ Unverified Claim"
		}

		fmt.Fprintf(out, "| `%s` (`%s`) | %s | **%s** (%d) | %s | %d | %s |\n",
			sym.Name, sym.FilePath, sym.ChangeType, sym.RiskLevel, sym.RiskScore, tierBadge, callersCount, testsLabel)
	}

	if len(rep.SuggestedTestSuite) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "## 🧪 Recommended Test Suite")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "```bash")
		for _, cmd := range rep.SuggestedTestSuite {
			fmt.Fprintln(out, cmd)
		}
		fmt.Fprintln(out, "```")
	}

	return nil
}

func renderChangeRadarJSON(out io.Writer, rep ChangeRadarReport) error {
	encoded, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(termsafe.NewJSONWriter(out), string(encoded))
	return nil
}
