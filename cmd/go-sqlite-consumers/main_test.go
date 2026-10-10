package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dnl-fm/go-sqlite/internal/consumers"
)

func TestRunJSONAndCheckBehavior(t *testing.T) {
	root := t.TempDir()
	writeCLIGoMod(t, filepath.Join(root, "outdated"), `module example.com/outdated

go 1.26.3

require github.com/dnl-fm/go-sqlite v0.7.4
`)
	writeCLIGoMod(t, filepath.Join(root, "local"), `module example.com/local

go 1.26.3

require github.com/dnl-fm/go-sqlite v0.0.0

replace github.com/dnl-fm/go-sqlite => ../source
`)

	resolverCalls := 0
	resolver := consumers.LatestResolverFunc(func(context.Context) (string, error) {
		resolverCalls++
		return "v0.8.0", nil
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(context.Background(), []string{"scan", root, "--json", "--check"}, &stdout, &stderr, resolver)
	if exitCode != 1 {
		t.Fatalf("run() exit = %d, want 1 for outdated published pin", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if resolverCalls != 1 {
		t.Fatalf("resolver calls = %d, want 1", resolverCalls)
	}
	if strings.HasPrefix(stdout.String(), "\n") {
		t.Fatalf("JSON output has decorative padding: %q", stdout.String())
	}
	var report consumers.Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout.String())
	}
	if report.Summary.Outdated != 1 || report.Summary.LocalReplace != 1 || report.Summary.Actionable != 1 {
		t.Fatalf("summary = %#v", report.Summary)
	}
}

func TestRunExplicitTargetAcceptsFlagsAroundRoot(t *testing.T) {
	root := t.TempDir()
	writeCLIGoMod(t, root, `module example.com/current

go 1.26.3

require github.com/dnl-fm/go-sqlite v0.8.0
`)
	resolver := consumers.LatestResolverFunc(func(context.Context) (string, error) {
		t.Fatal("resolver called with explicit target")
		return "", nil
	})

	for _, arguments := range [][]string{
		{"scan", root, "--target", "v0.8.0", "--check"},
		{"scan", "--target=v0.8.0", "--check", root},
	} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if exitCode := run(context.Background(), arguments, &stdout, &stderr, resolver); exitCode != 0 {
			t.Fatalf("run(%v) exit = %d, stderr=%q", arguments, exitCode, stderr.String())
		}
		if !strings.HasPrefix(stdout.String(), "\n") || !strings.Contains(stdout.String(), "current") {
			t.Fatalf("human stdout = %q", stdout.String())
		}
	}
}

func TestRunCheckIgnoresOnlyLocalReplacement(t *testing.T) {
	root := t.TempDir()
	writeCLIGoMod(t, root, `module example.com/local

go 1.26.3

require github.com/dnl-fm/go-sqlite v0.0.0

replace github.com/dnl-fm/go-sqlite => ./local-go-sqlite
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(
		context.Background(),
		[]string{"scan", root, "--target", "v0.8.0", "--check"},
		&stdout,
		&stderr,
		consumers.LatestResolverFunc(func(context.Context) (string, error) { return "", nil }),
	)
	if exitCode != 0 {
		t.Fatalf("run() exit = %d, want 0; stderr=%q", exitCode, stderr.String())
	}
}

func TestRunMissingRootIsCommandError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	resolver := consumers.LatestResolverFunc(func(context.Context) (string, error) {
		t.Fatal("resolver called for a missing root")
		return "", nil
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(context.Background(), []string{"scan", missing}, &stdout, &stderr, resolver)
	if exitCode != 1 {
		t.Fatalf("run() exit = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "inspect scan root") {
		t.Fatalf("stderr = %q, want missing root error", stderr.String())
	}
}

func TestParseScanArgumentsRejectsExtraRoot(t *testing.T) {
	_, err := parseScanArguments([]string{"one", "two"})
	if err == nil || !strings.Contains(err.Error(), "only one scan root") {
		t.Fatalf("parseScanArguments() error = %v", err)
	}
}

func writeCLIGoMod(t *testing.T, directory, contents string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
