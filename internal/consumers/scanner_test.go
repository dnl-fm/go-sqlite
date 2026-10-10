package consumers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestScannerClassifieswerkConsumers(t *testing.T) {
	root := t.TempDir()
	localSource := filepath.Join(root, "sources", "go-sqlite")
	writeGoMod(t, localSource, "module "+ModulePath+"\n\ngo 1.26.3\n")

	writeConsumer(t, root, "a-current", "example.com/current", `
require github.com/dnl-fm/go-sqlite v0.8.0
`)
	writeConsumer(t, root, "b-outdated", "example.com/outdated", `
require github.com/dnl-fm/go-sqlite v0.7.4
`)
	writeConsumer(t, root, "c-indirect", "example.com/indirect", `
require github.com/dnl-fm/go-sqlite v0.0.0 // indirect
`)
	writeConsumer(t, root, "d-local-relative", "example.com/local-relative", `
require github.com/dnl-fm/go-sqlite v0.0.0

replace github.com/dnl-fm/go-sqlite => ../../sources/go-sqlite
`)
	writeConsumer(t, root, "e-local-absolute", "example.com/local-absolute", fmt.Sprintf(`
require github.com/dnl-fm/go-sqlite v0.7.4

replace github.com/dnl-fm/go-sqlite => %s
`, localSource))
	writeConsumer(t, root, "f-legacy", "example.com/legacy-consumer", `
require example.com/old-sqlite v0.2.0 // indirect

replace example.com/old-sqlite => ../sources/go-sqlite
`)
	writeConsumer(t, root, "f-legacy-absolute", "example.com/legacy-absolute", fmt.Sprintf(`
require example.com/old-sqlite-absolute v0.1.0

replace example.com/old-sqlite-absolute => %s
`, localSource))
	writeConsumer(t, root, "g-repeated", "example.com/repeated", `
require github.com/dnl-fm/go-sqlite v0.6.0

replace github.com/dnl-fm/go-sqlite => ../wildcard
replace github.com/dnl-fm/go-sqlite v0.6.0 => ../exact
`)
	writeConsumer(t, root, "archive/kept", "example.com/archived", `
require github.com/dnl-fm/go-sqlite v0.7.1
`)

	writeConsumer(t, root, "vendor/ignored", "example.com/vendor", `
require github.com/dnl-fm/go-sqlite v0.1.0
`)
	writeConsumer(t, root, "node_modules/ignored", "example.com/node", `
require github.com/dnl-fm/go-sqlite v0.1.0
`)
	writeConsumer(t, root, ".git/ignored", "example.com/git", `
require github.com/dnl-fm/go-sqlite v0.1.0
`)
	writeConsumer(t, root, ".docker/ignored", "example.com/docker", `
require github.com/dnl-fm/go-sqlite v0.1.0
`)
	writeConsumer(t, root, ".provision-deploy/ignored", "example.com/deploy", `
require github.com/dnl-fm/go-sqlite v0.1.0
`)
	writeConsumer(t, root, ".runtime/ignored", "example.com/runtime", `
require github.com/dnl-fm/go-sqlite v0.1.0
`)
	writeConsumer(t, root, "h-missing-replace", "example.com/missing-replace", `
require example.com/old-sqlite-missing v0.1.0

replace example.com/old-sqlite-missing => ../missing-source
`)

	malformedPath := filepath.Join(root, "malformed")
	writeGoMod(t, malformedPath, "this is not a go.mod\n")

	resolverCalls := 0
	scanner := Scanner{Resolver: LatestResolverFunc(func(context.Context) (string, error) {
		resolverCalls++
		return "v0.8.0", nil
	})}
	report, err := scanner.Scan(context.Background(), ScanOptions{Root: root})
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if resolverCalls != 1 {
		t.Fatalf("latest resolver calls = %d, want 1", resolverCalls)
	}

	if report.Target != "v0.8.0" {
		t.Errorf("target = %q, want v0.8.0", report.Target)
	}
	if report.Summary != (Summary{Total: 9, Current: 1, Outdated: 3, LocalReplace: 3, LegacyReplace: 2, Actionable: 3}) {
		t.Errorf("summary = %#v", report.Summary)
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Path != filepath.Join(malformedPath, "go.mod") {
		t.Fatalf("diagnostics = %#v, want malformed go.mod diagnostic", report.Diagnostics)
	}

	byModule := make(map[string]*Result, len(report.Results))
	for index := range report.Results {
		result := &report.Results[index]
		byModule[result.ModulePath] = result
	}
	assertResult(t, byModule["example.com/current"], StatusCurrent, "direct", "", false)
	assertResult(t, byModule["example.com/outdated"], StatusOutdated, "direct", "", true)
	assertResult(t, byModule["example.com/indirect"], StatusOutdated, "indirect", "", true)
	assertResult(t, byModule["example.com/local-relative"], StatusLocalReplace, "direct", "../../sources/go-sqlite", false)
	assertResult(t, byModule["example.com/local-absolute"], StatusLocalReplace, "direct", localSource, false)
	assertResult(t, byModule["example.com/legacy-consumer"], StatusLegacyReplace, "indirect", "../sources/go-sqlite", false)
	assertResult(t, byModule["example.com/legacy-absolute"], StatusLegacyReplace, "direct", localSource, false)
	assertResult(t, byModule["example.com/repeated"], StatusLocalReplace, "direct", "../exact", false)
	assertResult(t, byModule["example.com/archived"], StatusOutdated, "direct", "", true)

	for index := 1; index < len(report.Results); index++ {
		if report.Results[index-1].ModuleDir > report.Results[index].ModuleDir {
			t.Fatalf("results are not sorted: %q before %q", report.Results[index-1].ModuleDir, report.Results[index].ModuleDir)
		}
	}
	if strings.Contains(strings.Join(resultModulePaths(report.Results), ","), "ignored") {
		t.Fatalf("ignored directory consumer leaked into results: %#v", report.Results)
	}
}

func TestScannerUsesExplicitTargetWithoutResolver(t *testing.T) {
	root := t.TempDir()
	writeConsumer(t, root, "app", "example.com/app", `
require github.com/dnl-fm/go-sqlite v0.8.1
`)

	scanner := Scanner{Resolver: LatestResolverFunc(func(context.Context) (string, error) {
		t.Fatal("resolver must not be called with an explicit target")
		return "", nil
	})}
	report, err := scanner.Scan(context.Background(), ScanOptions{Root: root, Target: "v0.8.0"})
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if got := report.Results[0].Status; got != StatusCurrent {
		t.Fatalf("status = %q, want current for a pin above target", got)
	}
}

func TestScannerRejectsInvalidTarget(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), ScanOptions{Root: t.TempDir(), Target: "0.8.0"})
	if err == nil || !strings.Contains(err.Error(), "not a valid Go module version") {
		t.Fatalf("Scan() error = %v, want invalid version error", err)
	}
}

func TestScannerRejectsMissingAndFileRoots(t *testing.T) {
	parent := t.TempDir()
	missing := filepath.Join(parent, "missing")
	_, err := (Scanner{}).Scan(context.Background(), ScanOptions{Root: missing, Target: "v0.8.0"})
	if err == nil || !strings.Contains(err.Error(), "inspect scan root") {
		t.Fatalf("missing root error = %v", err)
	}

	fileRoot := filepath.Join(parent, "not-a-directory")
	if writeErr := os.WriteFile(fileRoot, []byte("file"), 0o600); writeErr != nil {
		t.Fatalf("WriteFile() error = %v", writeErr)
	}
	_, err = (Scanner{}).Scan(context.Background(), ScanOptions{Root: fileRoot, Target: "v0.8.0"})
	if err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Fatalf("file root error = %v", err)
	}
}

func TestReportOutputIsStable(t *testing.T) {
	report := Report{
		Root:   "/tmp/werk",
		Target: "v0.8.0",
		Results: []Result{
			{
				ModulePath:      "example.com/app",
				ModuleDir:       "/tmp/werk/app",
				GoModPath:       "/tmp/werk/app/go.mod",
				DependencyPath:  ModulePath,
				RequiredVersion: "v0.7.4",
				ReferenceKind:   "require",
				DependencyKind:  "direct",
				Status:          StatusOutdated,
				UpgradeCommand:  "cd /tmp/werk/app && upgrade",
			},
		},
		Diagnostics: []Diagnostic{{Path: "/tmp/werk/bad/go.mod", Message: "bad module"}},
		Summary:     Summary{Total: 1, Outdated: 1, Actionable: 1},
	}

	var firstJSON bytes.Buffer
	if err := WriteJSON(&firstJSON, &report); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
	var secondJSON bytes.Buffer
	if err := WriteJSON(&secondJSON, &report); err != nil {
		t.Fatalf("WriteJSON() second error = %v", err)
	}
	if firstJSON.String() != secondJSON.String() {
		t.Fatalf("JSON output is unstable:\n%s\n%s", firstJSON.String(), secondJSON.String())
	}
	var decoded Report
	if err := json.Unmarshal(firstJSON.Bytes(), &decoded); err != nil {
		t.Fatalf("JSON is invalid: %v", err)
	}
	if !reflect.DeepEqual(decoded, report) {
		t.Fatalf("decoded report = %#v, want %#v", decoded, report)
	}

	var human bytes.Buffer
	if err := WriteHuman(&human, &report); err != nil {
		t.Fatalf("WriteHuman() error = %v", err)
	}
	output := human.String()
	if !strings.HasPrefix(output, "\n") || !strings.HasSuffix(output, "\n\n") {
		t.Fatalf("human output is not padded with blank lines: %q", output)
	}
	for _, expected := range []string{
		"STATUS", "--", "outdated", "Upgrade commands:", report.Results[0].UpgradeCommand,
		"Diagnostics:", report.Diagnostics[0].Path, "Total: 1",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("human output missing %q:\n%s", expected, output)
		}
	}
}

func TestUpgradeCommandQuotesModuleDirectory(t *testing.T) {
	command := upgradeCommand("/tmp/My werk/app's module", "v0.8.0")
	wantPrefix := `cd '/tmp/My werk/app'"'"'s module' && `
	if !strings.HasPrefix(command, wantPrefix) {
		t.Fatalf("upgrade command = %q, want prefix %q", command, wantPrefix)
	}
}

func TestReplaceEnvironmentValueOverridesExistingValue(t *testing.T) {
	got := replaceEnvironmentValue([]string{"PATH=/bin", "GOWORK=old", "GOWORK=older"}, "GOWORK", "off")
	want := []string{"PATH=/bin", "GOWORK=off"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("replaceEnvironmentValue() = %#v, want %#v", got, want)
	}
}

func TestHumanOutputExplainsNonActionableReplacements(t *testing.T) {
	report := Report{
		Target: "v0.8.0",
		Results: []Result{
			{
				ModuleDir:         "/tmp/local",
				DependencyPath:    ModulePath,
				RequiredVersion:   "v0.0.0",
				DependencyKind:    "direct",
				ReplacementTarget: "../go-sqlite",
				Status:            StatusLocalReplace,
			},
			{
				ModuleDir:         "/tmp/legacy",
				DependencyPath:    "example.com/old-sqlite",
				RequiredVersion:   "v0.2.0",
				DependencyKind:    "direct",
				ReplacementTarget: "/tmp/go-sqlite",
				Status:            StatusLegacyReplace,
			},
		},
		Summary: Summary{Total: 2, LocalReplace: 1, LegacyReplace: 1},
	}

	var output bytes.Buffer
	if err := WriteHuman(&output, &report); err != nil {
		t.Fatalf("WriteHuman() error = %v", err)
	}
	for _, expected := range []string{
		"version v0.0.0 is not effective",
		"example.com/old-sqlite points at local go-sqlite source",
		"migrate manually (no automatic rewrite)",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("human output missing %q:\n%s", expected, output.String())
		}
	}
}

func writeConsumer(t *testing.T, root, relativeDir, modulePath, body string) {
	t.Helper()
	writeGoMod(t, filepath.Join(root, filepath.FromSlash(relativeDir)), fmt.Sprintf("module %s\n\ngo 1.26.3\n%s", modulePath, body))
}

func writeGoMod(t *testing.T, directory, contents string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", directory, err)
	}
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(go.mod) error = %v", err)
	}
}

func assertResult(t *testing.T, result *Result, status Status, dependencyKind, replacement string, actionable bool) {
	t.Helper()
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.Status != status || result.DependencyKind != dependencyKind || result.ReplacementTarget != replacement {
		t.Errorf("result = %#v, want status=%q kind=%q replacement=%q", result, status, dependencyKind, replacement)
	}
	if actionable != (result.UpgradeCommand != "") {
		t.Errorf("result actionability = %v, want %v: %#v", result.UpgradeCommand != "", actionable, result)
	}
}

func resultModulePaths(results []Result) []string {
	paths := make([]string, 0, len(results))
	for index := range results {
		paths = append(paths, results[index].ModulePath)
	}
	return paths
}
