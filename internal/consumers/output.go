package consumers

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
)

// WriteJSON writes one stable JSON report.
func WriteJSON(writer io.Writer, report *Report) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("write JSON report: %w", err)
	}
	return nil
}

// WriteHuman writes a padded human-readable report.
func WriteHuman(writer io.Writer, report *Report) error {
	writers := []func(io.Writer, *Report) error{
		writeConsumerTable,
		writeSummary,
		writeUpgradeCommands,
		writeReplacementNotes,
		writeDiagnostics,
	}
	for _, writeSection := range writers {
		if err := writeSection(writer, report); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(writer); err != nil {
		return fmt.Errorf("finish human report: %w", err)
	}
	return nil
}

func writeConsumerTable(writer io.Writer, report *Report) error {
	if _, err := fmt.Fprintln(writer); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "go-sqlite consumers (target %s)\n\n", report.Target); err != nil {
		return err
	}
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "STATUS\tVERSION\tKIND\tMODULE DIRECTORY"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(table, "--\t--\t--\t--"); err != nil {
		return err
	}
	if len(report.Results) == 0 {
		if _, err := fmt.Fprintln(table, "--\t--\t--\t(no consumers found)"); err != nil {
			return err
		}
	}
	for index := range report.Results {
		result := &report.Results[index]
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", result.Status, result.RequiredVersion, result.DependencyKind, result.ModuleDir); err != nil {
			return err
		}
	}
	if err := table.Flush(); err != nil {
		return fmt.Errorf("write consumer table: %w", err)
	}
	return nil
}

func writeSummary(writer io.Writer, report *Report) error {
	_, err := fmt.Fprintf(
		writer,
		"\nTotal: %d (current %d, outdated %d, local-replace %d, legacy-replace %d)\n",
		report.Summary.Total,
		report.Summary.Current,
		report.Summary.Outdated,
		report.Summary.LocalReplace,
		report.Summary.LegacyReplace,
	)
	return err
}

func writeUpgradeCommands(writer io.Writer, report *Report) error {
	if report.Summary.Outdated == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(writer, "\nUpgrade commands:"); err != nil {
		return err
	}
	for index := range report.Results {
		command := report.Results[index].UpgradeCommand
		if command == "" {
			continue
		}
		if _, err := fmt.Fprintf(writer, "  %s\n", command); err != nil {
			return err
		}
	}
	return nil
}

func writeReplacementNotes(writer io.Writer, report *Report) error {
	if report.Summary.LocalReplace == 0 && report.Summary.LegacyReplace == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(writer, "\nReplacement notes:"); err != nil {
		return err
	}
	for index := range report.Results {
		if err := writeReplacementNote(writer, &report.Results[index]); err != nil {
			return err
		}
	}
	return nil
}

func writeReplacementNote(writer io.Writer, result *Result) error {
	switch result.Status {
	case StatusLocalReplace:
		_, err := fmt.Fprintf(
			writer,
			"  %s: version %s is not effective; local replacement %s supplies the code.\n",
			result.ModuleDir,
			result.RequiredVersion,
			result.ReplacementTarget,
		)
		return err
	case StatusLegacyReplace:
		_, err := fmt.Fprintf(
			writer,
			"  %s: %s points at local go-sqlite source %s; migrate manually (no automatic rewrite).\n",
			result.ModuleDir,
			result.DependencyPath,
			result.ReplacementTarget,
		)
		return err
	case StatusCurrent, StatusOutdated:
		return nil
	}
	return nil
}

func writeDiagnostics(writer io.Writer, report *Report) error {
	if len(report.Diagnostics) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(writer, "\nDiagnostics:"); err != nil {
		return err
	}
	for index := range report.Diagnostics {
		diagnostic := &report.Diagnostics[index]
		if _, err := fmt.Fprintf(writer, "  %s: %s\n", diagnostic.Path, diagnostic.Message); err != nil {
			return err
		}
	}
	return nil
}
