package consumers

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

// ModulePath is the dependency located by consumer scans.
const ModulePath = "github.com/dnl-fm/go-sqlite"

// Status describes whether a consumer needs an automatic or manual upgrade.
type Status string

// Consumer classification values.
const (
	StatusCurrent       Status = "current"
	StatusOutdated      Status = "outdated"
	StatusLocalReplace  Status = "local-replace"
	StatusLegacyReplace Status = "legacy-replace"
)

// LatestResolver resolves the latest published go-sqlite version.
type LatestResolver interface {
	Latest(ctx context.Context) (string, error)
}

// LatestResolverFunc adapts a function to LatestResolver.
type LatestResolverFunc func(context.Context) (string, error)

// Latest resolves the latest published version through f.
func (f LatestResolverFunc) Latest(ctx context.Context) (string, error) {
	return f(ctx)
}

// Scanner discovers go-sqlite consumers below a root directory.
type Scanner struct {
	Resolver LatestResolver
}

// ScanOptions configures a consumer scan.
type ScanOptions struct {
	Root   string
	Target string
}

type preparedScan struct {
	root   string
	target string
}

// Result describes one go-sqlite consumer reference.
type Result struct {
	ModulePath        string `json:"modulePath"`
	ModuleDir         string `json:"moduleDir"`
	GoModPath         string `json:"goModPath"`
	DependencyPath    string `json:"dependencyPath"`
	RequiredVersion   string `json:"requiredVersion"`
	ReferenceKind     string `json:"referenceKind"`
	DependencyKind    string `json:"dependencyKind"`
	ReplacementTarget string `json:"replacementTarget,omitempty"`
	Status            Status `json:"status"`
	UpgradeCommand    string `json:"upgradeCommand,omitempty"`
}

// Diagnostic records a module that could not be inspected.
type Diagnostic struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Summary contains deterministic totals for each classification.
type Summary struct {
	Total         int `json:"total"`
	Current       int `json:"current"`
	Outdated      int `json:"outdated"`
	LocalReplace  int `json:"localReplace"`
	LegacyReplace int `json:"legacyReplace"`
	Actionable    int `json:"actionable"`
}

// Report is the stable machine-readable result of a scan.
type Report struct {
	Root        string       `json:"root"`
	Target      string       `json:"target"`
	Results     []Result     `json:"results"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Summary     Summary      `json:"summary"`
}

// Scan walks the configured root and classifies every go-sqlite consumer.
func (s Scanner) Scan(ctx context.Context, options ScanOptions) (Report, error) {
	prepared, err := s.prepare(ctx, options)
	if err != nil {
		return Report{}, err
	}
	report := Report{
		Root:        prepared.root,
		Target:      prepared.target,
		Results:     []Result{},
		Diagnostics: []Diagnostic{},
	}
	if err := walkModules(prepared.root, prepared.target, &report); err != nil {
		return Report{}, err
	}
	sortReport(&report)
	report.Summary = summarize(report.Results)
	return report, nil
}

func (s Scanner) prepare(ctx context.Context, options ScanOptions) (preparedScan, error) {
	if strings.TrimSpace(options.Root) == "" {
		return preparedScan{}, errors.New("scan root is required")
	}
	root, err := filepath.Abs(options.Root)
	if err != nil {
		return preparedScan{}, fmt.Errorf("resolve scan root: %w", err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return preparedScan{}, fmt.Errorf("inspect scan root %s: %w", root, err)
	}
	if !rootInfo.IsDir() {
		return preparedScan{}, fmt.Errorf("scan root %s is not a directory", root)
	}
	target, err := s.resolveTarget(ctx, options.Target)
	if err != nil {
		return preparedScan{}, err
	}
	return preparedScan{root: root, target: target}, nil
}

func (s Scanner) resolveTarget(ctx context.Context, target string) (string, error) {
	if target == "" {
		if s.Resolver == nil {
			return "", errors.New("latest resolver is required when target is empty")
		}
		var err error
		target, err = s.Resolver.Latest(ctx)
		if err != nil {
			return "", fmt.Errorf("resolve %s@latest: %w", ModulePath, err)
		}
	}
	if !semver.IsValid(target) {
		return "", fmt.Errorf("target %q is not a valid Go module version", target)
	}
	return target, nil
}

func walkModules(root, target string, report *Report) error {
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			report.Diagnostics = append(report.Diagnostics, Diagnostic{Path: path, Message: walkErr.Error()})
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if path != root && isIgnoredDirectory(entry.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Name() != "go.mod" {
			return nil
		}
		results, diagnostics := scanModule(path, target)
		report.Results = append(report.Results, results...)
		report.Diagnostics = append(report.Diagnostics, diagnostics...)
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk %s: %w", root, err)
	}
	return nil
}

func isIgnoredDirectory(name string) bool {
	switch name {
	case ".git", ".docker", ".provision-deploy", ".runtime", "vendor", "node_modules":
		return true
	default:
		return false
	}
}

func sortReport(report *Report) {
	slices.SortStableFunc(report.Results, func(a, b Result) int {
		if order := strings.Compare(a.ModuleDir, b.ModuleDir); order != 0 {
			return order
		}
		if order := strings.Compare(a.ReferenceKind, b.ReferenceKind); order != 0 {
			return order
		}
		if order := strings.Compare(a.DependencyKind, b.DependencyKind); order != 0 {
			return order
		}
		if order := strings.Compare(a.ReplacementTarget, b.ReplacementTarget); order != 0 {
			return order
		}
		return strings.Compare(a.RequiredVersion, b.RequiredVersion)
	})
	slices.SortStableFunc(report.Diagnostics, func(a, b Diagnostic) int {
		if order := strings.Compare(a.Path, b.Path); order != 0 {
			return order
		}
		return strings.Compare(a.Message, b.Message)
	})
}

func summarize(results []Result) Summary {
	var summary Summary
	for index := range results {
		summary.Total++
		switch results[index].Status {
		case StatusCurrent:
			summary.Current++
		case StatusOutdated:
			summary.Outdated++
			summary.Actionable++
		case StatusLocalReplace:
			summary.LocalReplace++
		case StatusLegacyReplace:
			summary.LegacyReplace++
		}
	}
	return summary
}

func scanModule(goModPath, target string) ([]Result, []Diagnostic) {
	parsed, diagnostic := parseModule(goModPath)
	if diagnostic != nil {
		return nil, []Diagnostic{*diagnostic}
	}
	moduleDir := filepath.Dir(goModPath)
	results := make([]Result, 0, 1)
	diagnostics := []Diagnostic{}
	for _, requirement := range parsed.Require {
		if requirement.Mod.Path == ModulePath {
			results = append(results, classifyRequirement(parsed, requirement, moduleDir, goModPath, target))
			continue
		}
		result, legacyDiagnostic := classifyLegacyRequirement(parsed, requirement, moduleDir, goModPath)
		if legacyDiagnostic != nil {
			diagnostics = append(diagnostics, *legacyDiagnostic)
		}
		if result != nil {
			results = append(results, *result)
		}
	}
	return results, diagnostics
}

func parseModule(goModPath string) (*modfile.File, *Diagnostic) {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return nil, &Diagnostic{Path: goModPath, Message: fmt.Sprintf("read go.mod: %v", err)}
	}
	parsed, err := modfile.Parse(goModPath, data, nil)
	if err != nil {
		return nil, &Diagnostic{Path: goModPath, Message: fmt.Sprintf("parse go.mod: %v", err)}
	}
	if parsed.Module == nil || parsed.Module.Mod.Path == "" {
		return nil, &Diagnostic{Path: goModPath, Message: "parse go.mod: module declaration is missing"}
	}
	return parsed, nil
}

func classifyRequirement(parsed *modfile.File, requirement *modfile.Require, moduleDir, goModPath, target string) Result {
	replacement := effectiveReplace(parsed.Replace, requirement.Mod.Path, requirement.Mod.Version)
	result := baseResult(parsed.Module.Mod.Path, moduleDir, goModPath, requirement)
	result.ReferenceKind = "require"
	if replacement != nil {
		result.ReplacementTarget = replacementString(replacement)
	}
	switch {
	case replacement != nil && isLocalReplacement(replacement):
		result.Status = StatusLocalReplace
	case semver.Compare(requirement.Mod.Version, target) < 0:
		result.Status = StatusOutdated
		result.UpgradeCommand = upgradeCommand(moduleDir, target)
	default:
		result.Status = StatusCurrent
	}
	return result
}

func classifyLegacyRequirement(
	parsed *modfile.File,
	requirement *modfile.Require,
	moduleDir string,
	goModPath string,
) (*Result, *Diagnostic) {
	replacement := effectiveReplace(parsed.Replace, requirement.Mod.Path, requirement.Mod.Version)
	if replacement == nil || !isLocalReplacement(replacement) {
		return nil, nil
	}
	localGoMod := replacementGoModPath(moduleDir, replacement.New.Path)
	declaredPath, err := readModulePath(localGoMod)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, &Diagnostic{Path: localGoMod, Message: err.Error()}
	}
	if declaredPath != ModulePath {
		return nil, nil
	}
	result := baseResult(parsed.Module.Mod.Path, moduleDir, goModPath, requirement)
	result.ReferenceKind = "legacy-replace"
	result.ReplacementTarget = replacementString(replacement)
	result.Status = StatusLegacyReplace
	return &result, nil
}

func replacementGoModPath(moduleDir, replacementPath string) string {
	if !filepath.IsAbs(replacementPath) {
		replacementPath = filepath.Join(moduleDir, replacementPath)
	}
	return filepath.Clean(filepath.Join(replacementPath, "go.mod"))
}

func baseResult(modulePath, moduleDir, goModPath string, requirement *modfile.Require) Result {
	const dependencyKindDirect = "direct"

	dependencyKind := dependencyKindDirect
	if requirement.Indirect {
		dependencyKind = "indirect"
	}
	return Result{
		ModulePath:      modulePath,
		ModuleDir:       moduleDir,
		GoModPath:       goModPath,
		DependencyPath:  requirement.Mod.Path,
		RequiredVersion: requirement.Mod.Version,
		DependencyKind:  dependencyKind,
	}
}

func effectiveReplace(replacements []*modfile.Replace, path, version string) *modfile.Replace {
	var wildcard *modfile.Replace
	for _, replacement := range replacements {
		if replacement.Old.Path != path {
			continue
		}
		if replacement.Old.Version == version {
			return replacement
		}
		if replacement.Old.Version == "" {
			wildcard = replacement
		}
	}
	return wildcard
}

func isLocalReplacement(replacement *modfile.Replace) bool {
	return replacement.New.Version == ""
}

func replacementString(replacement *modfile.Replace) string {
	if replacement.New.Version == "" {
		return replacement.New.Path
	}
	return replacement.New.Path + "@" + replacement.New.Version
}

func readModulePath(goModPath string) (string, error) {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return "", fmt.Errorf("read replacement go.mod: %w", err)
	}
	parsed, err := modfile.Parse(goModPath, data, nil)
	if err != nil {
		return "", fmt.Errorf("parse replacement go.mod: %w", err)
	}
	if parsed.Module == nil || parsed.Module.Mod.Path == "" {
		return "", errors.New("parse replacement go.mod: module declaration is missing")
	}
	return parsed.Module.Mod.Path, nil
}

func upgradeCommand(moduleDir, target string) string {
	return fmt.Sprintf(
		"cd %s && GOWORK=off go get %s@%s && GOWORK=off go mod tidy",
		shellQuote(moduleDir), ModulePath, target,
	)
}

func shellQuote(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\r\n'\"`$&;|<>*?()[]{}!\\") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
