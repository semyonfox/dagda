package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/semyonfox/dagda/engine/internal/ingest"
	"github.com/semyonfox/dagda/engine/internal/model"
	"github.com/semyonfox/dagda/engine/internal/pricing"
	"github.com/semyonfox/dagda/engine/internal/projectmeta"
	"github.com/semyonfox/dagda/engine/internal/report"
)

func cmdReport(cmd string, args []string) int {
	opts, err := parseReportOptions(cmd, args)
	if err == flag.ErrHelp {
		printHelp(cmd)
		return 0
	}
	if err != nil {
		return commandError(cmd, err)
	}
	f, agents := opts.filter, opts.filter.Agents
	// read imports before the scan so a bad file fails fast
	imported, err := readImports(opts.imports)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dagda: %v\n", err)
		return 1
	}

	progress := startProgress(os.Stderr, progressEnabled(opts.asJSON || opts.plainOutput), "Checking for updated prices...")
	defer progress.Stop()
	tbl, err := pricing.Load()
	if err != nil {
		progress.Stop()
		fmt.Fprintf(os.Stderr, "dagda: %v\n", err)
		return 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	progress.Update("Finding agent logs...")
	scanners := ingest.Available(agents...)
	if len(scanners) == 0 && len(opts.imports) == 0 {
		progress.Stop()
		fmt.Fprintln(os.Stderr, "dagda: "+errNoLogs)
		return 1
	}
	res, cacheDir, err := scanLocal(ctx, progress, scanners, opts.noCache)
	if err != nil {
		progress.Stop()
		fmt.Fprintf(os.Stderr, "dagda: %v\n", err)
		return 1
	}
	if len(opts.imports) > 0 {
		res.Import(imported)
	}
	lineage, lineageErr := projectmeta.LoadT3ProjectLineage(ctx)

	g := report.Daily
	switch cmd {
	case "weekly":
		g = report.Weekly
	case "monthly":
		g = report.Monthly
	}
	progress.Update("Calculating usage and costs...")
	rep := report.BuildWithProjectLineage(res.Turns, tbl, f, g, res.Duplicates, opts.dimensions, lineage)
	rep.ImportedDuplicates, rep.ImportedUnkeyed = res.ImportedDuplicates, res.ImportedUnkeyed
	if cmd == "summary" {
		// only the overview shows plan windows, so only it pays for the lookup;
		// expired ones are dropped here so JSON and the table agree
		windows, planErrs := ingest.PlanWindows(ctx, scanners)
		rep.PlanWindows = ingest.LivePlanWindows(windows, time.Now())
		res.Errors = append(res.Errors, planErrs...)
	}
	progress.Stop()
	if lineageErr != nil {
		fmt.Fprintf(os.Stderr, "dagda: warning: %v\n", lineageErr)
	}
	for _, e := range res.Errors {
		fmt.Fprintf(os.Stderr, "dagda: warning: %v\n", e)
	}
	for _, m := range rep.FilterMatches {
		switch {
		case m.How == "none":
			fmt.Fprintf(os.Stderr, "dagda: no %s matches %q (`dagda %s` lists them)\n", m.Filter, m.Query, m.Filter)
		case m.Loose():
			fmt.Fprintf(os.Stderr, "dagda: %s %q matched %s\n", m.Filter, m.Query, strings.Join(m.Matches, ", "))
		}
	}
	if opts.verbose && cacheDir != "" {
		fmt.Fprintf(os.Stderr, "dagda: scan cache: %d hit(s), %d miss(es)\n", res.CacheHits, res.CacheMisses)
	}

	if opts.asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(view(cmd, rep)); err != nil {
			fmt.Fprintf(os.Stderr, "dagda: %v\n", err)
			return 1
		}
		return 0
	}
	if cmd == "summary" {
		renderOverview(os.Stdout, rep, opts.limit, colorEnabled(opts.noColor), opts.verbose, !opts.plainOutput, agents)
		if opts.defaultWindow {
			// other reports default to all history, so say why this one is shorter
			fmt.Fprintln(os.Stdout, "\n    last 30 days by default · dagda -a for all history")
		}
		return 0
	}
	render(os.Stdout, cmd, rep, opts.limit, colorEnabled(opts.noColor), opts.verbose, opts.compact, agents)
	return 0
}

// readImports loads record files, with - for stdin, so an export can be piped
// straight in from another machine.
func readImports(paths []string) ([]model.Turn, error) {
	var all []model.Turn
	for _, path := range paths {
		in, name := io.Reader(os.Stdin), "stdin"
		if path != "-" {
			f, err := os.Open(path)
			if err != nil {
				return nil, err
			}
			defer f.Close()
			in, name = f, path
		}
		turns, sameReaders, err := ingest.ReadRecords(in)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if !sameReaders {
			fmt.Fprintf(os.Stderr, "dagda: warning: %s was exported by a different dagda build; a call whose identity changed between builds counts twice\n", name)
		}
		all = append(all, turns...)
	}
	return all, nil
}

const errNoLogs = "no agent logs found on this machine (try `dagda agents --installed`)"

// scanLocal reads this machine's agent logs through the scan cache.
func scanLocal(ctx context.Context, progress *progress, scanners []ingest.Scanner, noCache bool) (*ingest.Result, string, error) {
	if len(scanners) == 0 {
		return &ingest.Result{}, "", nil
	}
	progress.Update(fmt.Sprintf("Scanning logs from %d agents...", len(scanners)))
	cacheDir := ""
	if !noCache {
		if dir, err := os.UserCacheDir(); err == nil {
			cacheDir = filepath.Join(dir, "dagda", "scans")
		}
	}
	res, err := ingest.RunCached(ctx, scanners, cacheDir)
	return res, cacheDir, err
}

// view narrows the report to the rows the chosen command is about, so `--json`
// output does not carry four aggregates the caller did not ask for.
func view(cmd string, rep *report.Report) any {
	if cmd == "summary" {
		return rep
	}
	type out struct {
		Command string          `json:"command"`
		Rows    []report.Bucket `json:"rows"`
		*report.Report
	}
	// Work on a copy so trimming the aggregates below never mutates the
	// caller's report.
	narrowed := *rep
	o := out{Command: cmd, Report: &narrowed}
	switch cmd {
	case "session":
		o.Rows = rep.Sessions
	case "model":
		o.Rows = rep.ByModel
	case "project":
		o.Rows = rep.ByProject
	default:
		o.Rows = rep.Series
	}
	// Rows are the answer; the per-dimension aggregates stay available but are
	// not duplicated into Rows.
	o.Report.Series = nil
	if cmd != "model" {
		o.Report.ByModel = nil
	}
	if cmd != "project" {
		o.Report.ByProject = nil
	}
	if cmd != "session" {
		o.Report.Sessions = nil
	}
	return o
}
