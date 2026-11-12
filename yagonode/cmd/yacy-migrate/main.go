package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

var (
	commandArgs             = func() []string { return os.Args[1:] }
	commandErrors io.Writer = os.Stderr
	exitProcess             = os.Exit
)

func main() {
	exitProcess(execute())
}

func execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, commandArgs(), commandErrors); err != nil {
		if _, writeErr := fmt.Fprintln(commandErrors, err); writeErr != nil {
			return 2
		}
		return 1
	}
	return 0
}

func run(ctx context.Context, args []string, progress io.Writer) error {
	if len(args) == 0 {
		return errors.New(
			"usage: yacy-migrate export|import|import-frontier|rwi-backfill [options]",
		)
	}
	switch args[0] {
	case "export":
		return runExport(ctx, args[1:], progress)
	case "import":
		return runImport(ctx, args[1:], progress)
	case "import-frontier":
		return runFrontierImportCommand(ctx, args[1:], progress)
	case "rwi-backfill":
		return runRWIBackfillCommand(ctx, args[1:], progress)
	default:
		return fmt.Errorf(
			"unknown command %q; use export, import, import-frontier, or rwi-backfill",
			args[0],
		)
	}
}

func runExport(ctx context.Context, args []string, progress io.Writer) error {
	flags := flag.NewFlagSet("export", flag.ContinueOnError)
	flags.SetOutput(progress)
	opts := exportOptions{}
	flags.StringVar(&opts.SolrURL, "solr-url", "", "read-only YaCy Solr select endpoint")
	flags.StringVar(&opts.Output, "output", "", "private JSONL output path")
	flags.StringVar(
		&opts.SourceID,
		"source-id",
		"",
		"non-sensitive stable identifier for this export",
	)
	flags.IntVar(&opts.Rows, "rows", 16, "bounded Solr page size, 1 through 256")
	flags.BoolVar(&opts.DryRun, "dry-run", false, "query source count only; do not write files")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse export options: %w", err)
	}
	opts.User = os.Getenv("YACY_USERNAME")
	opts.Password = os.Getenv("YACY_PASSWORD")
	return exportCorpus(ctx, opts, progress)
}

func runImport(ctx context.Context, args []string, progress io.Writer) error {
	flags := flag.NewFlagSet("import", flag.ContinueOnError)
	flags.SetOutput(progress)
	opts := importOptions{}
	flags.StringVar(&opts.Input, "input", "", "versioned YaCy JSONL interchange file")
	flags.StringVar(&opts.Endpoint, "endpoint", "", "Yago POST /admin/index/import endpoint")
	flags.StringVar(&opts.State, "state", "", "checkpoint path; defaults beside input")
	flags.StringVar(
		&opts.FollowExportCheckpoint,
		"follow-export-checkpoint",
		"",
		"follow only output bytes committed by the named resumable export checkpoint",
	)
	flags.IntVar(
		&opts.BatchRecords,
		"batch-records",
		importMaximumRecords,
		"maximum records per destination request, 1 through 256",
	)
	flags.IntVar(
		&opts.MaxRecords,
		"max-records",
		0,
		"stop after this many successful records in this run; 0 means unlimited",
	)
	flags.BoolVar(
		&opts.DryRun,
		"dry-run",
		false,
		"validate remaining input without sending or checkpointing",
	)
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse import options: %w", err)
	}
	opts.APIKey = os.Getenv("YAGO_API_KEY")
	return importCorpus(ctx, opts, progress)
}

func runFrontierImportCommand(ctx context.Context, args []string, progress io.Writer) error {
	flags := flag.NewFlagSet("import-frontier", flag.ContinueOnError)
	flags.SetOutput(progress)
	opts := frontierImportOptions{}
	flags.StringVar(&opts.Input, "input", "", "private versioned YaCy queue JSONL file")
	flags.StringVar(&opts.Profiles, "profiles", "", "private YaCy crawl-profile JSONL sidecar")
	flags.StringVar(
		&opts.Endpoint,
		"endpoint",
		"",
		"Yago POST /admin/crawl/frontier/import endpoint",
	)
	flags.StringVar(&opts.State, "state", "", "checkpoint path; defaults beside input")
	flags.IntVar(
		&opts.BatchRecords,
		"batch-records",
		256,
		"maximum queue records per request, 1 through 256",
	)
	flags.IntVar(
		&opts.MaxRecords,
		"max-records",
		0,
		"stop after this many source records; 0 means unlimited",
	)
	flags.BoolVar(
		&opts.DryRun,
		"dry-run",
		false,
		"validate and count remaining input without sending or checkpointing",
	)
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse frontier import options: %w", err)
	}
	opts.APIKey = os.Getenv("YAGO_API_KEY")
	return importFrontier(ctx, opts, progress)
}
