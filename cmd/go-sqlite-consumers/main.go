package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dnl-fm/go-sqlite/internal/consumers"
)

const (
	commandScan  = "scan"
	optionCheck  = "--check"
	optionTarget = "--target"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, consumers.GoLatestResolver{}))
}

type scanArguments struct {
	root   string
	target string
	json   bool
	check  bool
	help   bool
}

func run(ctx context.Context, arguments []string, stdout, stderr io.Writer, resolver consumers.LatestResolver) int {
	if len(arguments) == 0 {
		printUsage(stderr)
		return 1
	}
	switch arguments[0] {
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	case commandScan:
		return runScan(ctx, arguments[1:], stdout, stderr, resolver)
	default:
		fmt.Fprintln(stderr, "Error: unknown command")
		fmt.Fprintln(stderr)
		printUsage(stderr)
		return 1
	}
}

func runScan(ctx context.Context, arguments []string, stdout, stderr io.Writer, resolver consumers.LatestResolver) int {
	parsed, err := parseScanArguments(arguments)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n\n", err)
		printScanUsage(stderr)
		return 1
	}
	if parsed.help {
		printScanUsage(stdout)
		return 0
	}
	report, err := (consumers.Scanner{Resolver: resolver}).Scan(ctx, consumers.ScanOptions{
		Root:   parsed.root,
		Target: parsed.target,
	})
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if err := writeReport(stdout, parsed.json, &report); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if parsed.check && report.Summary.Actionable > 0 {
		return 1
	}
	return 0
}

func writeReport(writer io.Writer, jsonOutput bool, report *consumers.Report) error {
	if jsonOutput {
		return consumers.WriteJSON(writer, report)
	}
	return consumers.WriteHuman(writer, report)
}

func parseScanArguments(arguments []string) (scanArguments, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return scanArguments{}, fmt.Errorf("resolve home directory: %w", err)
	}
	parsed := scanArguments{root: filepath.Join(home, "werk")}
	rootSet := false
	for len(arguments) > 0 {
		consumed, consumeErr := consumeScanArgument(arguments, &parsed, &rootSet)
		if consumeErr != nil {
			return scanArguments{}, consumeErr
		}
		arguments = arguments[consumed:]
	}
	return parsed, nil
}

func consumeScanArgument(arguments []string, parsed *scanArguments, rootSet *bool) (int, error) {
	argument := arguments[0]
	switch argument {
	case "--json":
		parsed.json = true
		return 1, nil
	case optionCheck:
		parsed.check = true
		return 1, nil
	case "--help", "-h":
		parsed.help = true
		return 1, nil
	case optionTarget:
		return consumeTarget(arguments, parsed)
	}
	if target, found := strings.CutPrefix(argument, optionTarget+"="); found {
		if target == "" {
			return 0, errors.New(optionTarget + " requires a version")
		}
		parsed.target = target
		return 1, nil
	}
	if strings.HasPrefix(argument, "-") {
		return 0, errors.New("unknown option")
	}
	if *rootSet {
		return 0, errors.New("only one scan root may be provided")
	}
	parsed.root = argument
	*rootSet = true
	return 1, nil
}

func consumeTarget(arguments []string, parsed *scanArguments) (int, error) {
	if len(arguments) < 2 {
		return 0, errors.New(optionTarget + " requires a version")
	}
	target := arguments[1]
	if strings.HasPrefix(target, "-") {
		return 0, errors.New(optionTarget + " requires a version")
	}
	parsed.target = target
	return 2, nil
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage: go-sqlite-consumers <command> [arguments]")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "Commands:")
	fmt.Fprintf(writer, "  %s [root]  Find go-sqlite consumers\n", commandScan)
	fmt.Fprintln(writer)
	fmt.Fprintf(writer, "Run 'go-sqlite-consumers %s --help' for scan options.\n", commandScan)
}

func printScanUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage: go-sqlite-consumers scan [root] [options]")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "Options:")
	fmt.Fprintln(writer, "  --target <version>  Compare against an explicit version")
	fmt.Fprintln(writer, "  --json              Emit one JSON document")
	fmt.Fprintln(writer, "  --check             Exit non-zero when published pins are outdated")
	fmt.Fprintln(writer, "  -h, --help          Show this help")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "The root defaults to $HOME/werk. Scans never edit consumer files.")
}
