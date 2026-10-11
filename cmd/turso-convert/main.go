package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "turso-convert:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		_, err := fmt.Fprintln(out, "Usage: turso-convert <src.db> <dst.db>\n       turso-convert --check <db>\nStop the owning service before conversion. The source is never modified.")
		return err
	}
	if len(args) != 2 {
		return fmt.Errorf("usage: turso-convert <src.db> <dst.db> | turso-convert --check <db>")
	}
	if args[0] == "--check" {
		kind, err := check(ctx, args[1])
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "%s: %s\n", args[1], kind)
		return err
	}
	r, err := convert(ctx, args[0], args[1])
	if err != nil {
		return err
	}
	kind := r.Kind
	if kind == "plain SQLite" {
		kind = "already SQLite"
	}
	_, err = fmt.Fprintf(out, "%s: %d tables, %d rows; %d source bytes -> %d SQLite bytes; %s\n", kind, r.Tables, r.Rows, r.SourceBytes, r.DestinationBytes, r.Duration)
	return err
}
