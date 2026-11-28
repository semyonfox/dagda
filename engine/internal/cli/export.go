package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mattn/go-isatty"

	"github.com/semyonfox/dagda/engine/internal/ingest"
	"github.com/semyonfox/dagda/engine/internal/model"
)

// cmdExport writes this machine's deduplicated records for another machine's
// --import. It is all history and unfiltered, because filters and prices are
// applied by the report that reads it.
func cmdExport(args []string) int {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var agents stringList
	fs.Var(&agents, "agent", "")
	noCache := fs.Bool("no-cache", false, "")
	var output string
	fs.StringVar(&output, "output", "", "")
	fs.StringVar(&output, "o", "", "")
	positional, err := parseFlags(fs, args)
	if err == flag.ErrHelp {
		printHelp("export")
		return 0
	}
	if err != nil {
		return commandError("export", err)
	}
	if len(positional) > 0 {
		return commandError("export", fmt.Errorf("export accepts no arguments; use --output FILE"))
	}
	for _, agent := range agents {
		if !ingest.KnownAgent(agent) {
			return commandError("export", fmt.Errorf("unknown agent %q; use `dagda agents` to list supported identifiers", agent))
		}
		if reason := ingest.SourceLimitation(agent); reason != "" {
			return commandError("export", fmt.Errorf("%s: %s", agent, reason))
		}
	}

	if output == "" && (isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())) {
		return commandError("export", fmt.Errorf("export writes compressed records; use --output FILE or pipe it into dagda --import -"))
	}
	scanners := ingest.Available(agents...)
	if len(scanners) == 0 {
		fmt.Fprintln(os.Stderr, "dagda: "+errNoLogs)
		return 1
	}
	progress := startProgress(os.Stderr, progressEnabled(false), "Finding agent logs...")
	defer progress.Stop()
	res, _, err := scanLocal(context.Background(), progress, scanners, *noCache)
	progress.Stop()
	if err != nil {
		fmt.Fprintf(os.Stderr, "dagda: %v\n", err)
		return 1
	}
	for _, e := range res.Errors {
		fmt.Fprintf(os.Stderr, "dagda: warning: %v\n", e)
	}
	if output == "" {
		err = ingest.WriteRecords(os.Stdout, res.Turns)
	} else {
		err = writeRecordsFile(output, res.Turns)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "dagda: %v\n", err)
		return 1
	}
	return 0
}

// writeRecordsFile writes the file itself rather than through the shell,
// because Windows PowerShell re-encodes redirected output as text and breaks
// the compressed bytes. A failed export leaves no partial file behind.
func writeRecordsFile(path string, turns []model.Turn) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := ingest.WriteRecords(tmp, turns); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
