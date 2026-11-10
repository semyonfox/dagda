# Changelog

## Unreleased

- Price Claude Mythos 5.1 and 5.2, Mythos 5.2 fast mode and Gemini 4 Carbon,
  which models.dev does not list, from a small hand-checked file of vendor list
  rates. Gemini 4 Carbon carries Google's announced rise on 1 December 2026.
  An upstream first-party rate replaces a hand-checked one when it appears.
- Stop billing cached tokens as free when the chosen reseller leaves out its
  cache-read price but another reseller quoting the same list price publishes
  one. A newly published cache-read price corrects the existing rate instead
  of starting a new dated one, so earlier usage is repriced too.
- Price dated Claude snapshot ids such as `claude-sonnet-4-20250514` at their
  undated model's rate when only resellers list the snapshot.
- Show `unpriced` instead of `$0.00` for a row with no rate at all.
  `dagda price` marks an announced future rate as `scheduled`, not `current`.

## 0.2.0 (2026-11-09)

First Dagda release. Prebuilt `dagda` binaries for Linux, macOS and Windows
on amd64 and arm64, checked against `checksums.txt`. Go users can run
`go install github.com/semyonfox/dagda/engine/cmd/dagda@latest`.

- Publish releases from a local `make publish`, with a tag-triggered workflow
  running the same script. `dagda version` reports the module version for
  `go install` builds.
- Colour the summary charts by agent: daily bars show list cost stacked per
  agent with a legend, and model and agent bars take their agent's colour.
  Agents use the nearest terminal colour to their own branding where it is
  recognisable, grey for Codex and orange for Claude among them, as distinct
  shades on a 256-colour terminal and the nearest basic colours otherwise,
  and free colours for the rest, never sharing one within a report. Without colour the
  bars stay plain. The daily chart reads cost with tokens
  alongside instead of tokens alone. JSON buckets gain `cost_by_agent`.
- Show each agent's own subscription windows in the summary: the five-hour and
  weekly percentages with reset times, Codex from its session logs and Claude
  Code through the new `dagda statusline` hook. `--json` carries them as
  `plan_windows`.
- Fit detailed tables to the terminal: text columns narrow, widest first, until
  the rows fit, and figures are never shortened. Output to a pipe or file is
  left whole; `COLUMNS` overrides the detected width. Summary charts now drop
  their bars from the detected width too, not only from `COLUMNS`.
- Give `--limit` one meaning: a cap on displayed rows, where 0 lifts the cap.
  Summary charts still default to seven rows when `--limit` is omitted.
- Remove the `--from`, `--to` and `--by` spellings in favour of `--since`,
  `--until` and `--group-by`. `--breakdown` combined with `--group-by` is now
  an error instead of silently winning. Help text columns line up.
- Group CLI help by task and add focused `dagda help COMMAND` / `COMMAND --help`.
  Add `agents --installed` and named catalog filters, plus compatible
  `sessions`, `models`, `projects` and `providers` aliases. Reject invalid
  flags and unknown agent identifiers before looking up prices or usage.
- Separate command routing, option parsing and report execution; consolidate
  provider registration and coverage in one registry. Add a documentation index
  and contributor guide while preserving report JSON and accounting behavior.

- Cache parsed usage locally to avoid repeatedly decoding unchanged histories;
  add `--no-cache` and cache diagnostics in `--verbose` reports.

- Add `--all-time` and `-a` to report commands, including `dagda -a`
  for an all-time summary. Reject combinations with explicit date bounds.

- Expand the provider catalog to 42 entries with 40 readers/importers. Mark
  Crush and Grokbot as source-limited instead of estimating their usage.
- Discover Cursor IDE and SDK SQLite stores locally, disclose missing counters,
  and keep optional exports separate from native history.
- Exclude explicitly linked Hermes/OpenClaw runtime mirrors from combined
  totals while retaining their records and respecting report filters.
- Preserve measured unclassified tokens and provider credits without inventing
  dollar values or historical model attribution.
- Read Antigravity metadata up to 64 MiB while extracting only accounting fields,
  and support optional stream-json captures as a replacement source.

- Add native local readers for GitHub Copilot's session store and session
  journals and VS Code storage, Grok Build's persisted usage ledger, and
  recognized Antigravity conversation metadata.
- Read optional Copilot CLI OpenTelemetry file exports for conversations
  without native usage, excluding overlapping records and metric rollups.
- Prefer Codex per-response usage records and preserve legacy turns in upgraded sessions.
- Reconcile complete Hermes call logs against database rows for daily attribution.
- Preserve distinct Hermes task/route rows and large session aggregates; disclose
  aggregate pricing and legacy replay assumptions in terminal and JSON reports.
- Add the default `summary` command with terminal charts and complete JSON output.
- Count sessions separately across agents when their session IDs coincide.
- Reject reversed date ranges, negative limits and unexpected report arguments.
- Fit long overview chart labels to the label column by terminal width and
  share one empty-result message across reports.
