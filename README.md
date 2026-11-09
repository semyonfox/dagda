# Dagda

See what your AI coding agents use and what it would cost at API list prices,
across 40 agents, from the logs already on your machine.

![dagda running the summary, daily and agents reports](docs/media/demo.gif)

<sub>Real usage from the maintainer's machine, with home paths shortened. The
dollar figures are API list value, mostly covered by subscriptions, not a bill.</sub>

## Why Dagda

In Irish mythology the Dagda is a chief figure among the Tuatha Dé Danann. One
of his treasures was a cauldron, the coire ansic or "undry" cauldron, said
never to run empty and to leave no company unsatisfied.

Token budgets on a subscription can feel the same way, until you add up real
usage and price it at list rates. Dagda does that adding up.

## Command name

The command is `dagda`. Shorter names were taken: `dd` is the Unix disk
utility, `dg` belongs to Dagster, DeployGate and others, `tt` to Tarantool and
`dgd` to the DGD MUD server. If you want something shorter, add a shell alias.

## Install

Linux or macOS (swap in `darwin` and `arm64` as needed):

```sh
curl -fsSL https://github.com/semyonfox/dagda/releases/latest/download/dagda_linux_amd64.tar.gz | tar -xz
mv dagda_linux_amd64/dagda ~/.local/bin/
```

With Go 1.26 or newer:

```sh
go install github.com/semyonfox/dagda/engine/cmd/dagda@latest
```

Windows zips, the other platforms and `checksums.txt` are on the
[releases page](https://github.com/semyonfox/dagda/releases).

## First commands

```sh
dagda                               # last 30 days: totals, daily cost, top models and agents
dagda daily --group-by agent,model  # every day, split by agent and model
dagda agents --installed            # which agents were found, and where
dagda -a --json                     # all history as JSON for your own scripts
```

## Why not ccusage?

[ccusage](https://github.com/ryoppippi/ccusage) is the established tool here and
is great if most of your usage is Claude Code. Dagda is for people who move
between agents. It reads Claude Code, Codex, Copilot CLI, Cursor, Cline, Roo,
Kilo, Qwen, Goose, Zed and 30 more into one report with one set of pricing
rules. It keeps unknown models unpriced rather than guessing, and it separates
subscription usage from metered API spend.

## How it works

```
 agent logs and SQLite       adapters           Go engine              output
 ~/.claude, ~/.codex,   ->  one reader   ->  normalise tokens,   ->  terminal charts
 Cursor, Copilot, Zed...    per agent        dedupe, price,          and tables, or
                                             aggregate by day        --json
```

Each adapter reads the files an agent already writes and turns them into the
same four token buckets: fresh input, output, cache reads and cache writes. The
engine removes duplicates, applies effective-dated list prices and builds the
report.

Your usage never leaves the machine. The only network request is a daily check
for updated model prices, which `DAGDA_OFFLINE=1` turns off.

## Screens

`dagda`, the 30-day summary:

![summary report](docs/media/summary.png)

`dagda daily --group-by agent,model --since 2026-11-07`:

![daily report grouped by agent and model](docs/media/daily.png)

`dagda agents --installed`:

![agents found on this machine](docs/media/agents.png)

## Supported agents

The [provider inventory](docs/provider-inventory.md) lists all 43 audited
adapters: 41 local readers and importers, plus documented source limitations
for Crush and Grokbot. Coverage includes Copilot CLI and supported VS Code
stores, Cursor local counters and CSV exports, Cline, Roo, Kilo, Qwen, Goose
and Zed. Some sources provide only session totals or credits.

Run `dagda agents` to see coverage and detected source paths.

## Using the reports

`dagda help` gives the command overview. `dagda help summary` or `dagda daily --help`
show the flags for one command. `dagda agents --installed` lists detected
sources, and `dagda agents cursor copilot` narrows the catalogue to named agents.
`sessions`, `models`, `projects` and `providers` are aliases for the singular
report commands and `agents`.

The default command is `summary`, covering the last 30 local calendar days. It
shows accounting totals, daily list-cost bars stacked by agent, and models and
agents ranked by API list cost. Each chart shows up to seven rows. `--limit N`
changes that number and `--limit 0` shows every row, without changing the
headline totals. Daily bars show the latest recorded days, and missing days are
not presented as verified zero usage. Each chart scales its bars independently.

Interactive reports show a startup spinner while checking prices, scanning logs
and calculating totals. It clears before the report appears and stays off for
redirected output.

`--plain` omits bars and the spinner. `--json` prints the complete report
without display limits. Bars also disappear when `COLUMNS` is set below 72.
`--no-color` and `NO_COLOR` disable colours. Reports print once and exit, with
no interactive terminal UI to manage.

`--all-time` or `-a` covers all available history, including undated records,
so `dagda -a` runs an all-time summary without naming the command. Explicit date
filters replace the summary's default window and cannot be combined with
`--all-time`. Detailed commands such as `daily` and `model` already include all
history unless filtered.

Repeated reports reuse a local cache of parsed usage for supported readers.
`--no-cache` forces a fresh scan and `--verbose` shows cache hits and misses.
See [cache coverage and invalidation](engine/README.md#scan-cache).

## Accounting

Each reader normalises usage into fresh input, output, cache reads and cache
writes. Every command uses the same pricing and aggregation code. Rates are
embedded, effective-dated and include supported context tiers and cache rates.
Unknown models and ambiguous session attribution keep their token counts but
contribute no invented cost. Measured tokens without a reliable split remain
unclassified. Kiro and Codebuff credits are reported separately from tokens and
dollars. See the inventory for import configuration and source-specific limits.

The dollar total is API list value, not a provider invoice. Subscription and
local usage are classified separately. Optional plan costs in
`~/.dagda/plans.json` are prorated over the matched activity window.
They do not establish actual invoiced spend or allocate a plan's cost to a
particular model or project. The summary also shows the five-hour and weekly
subscription windows an agent reports about itself: Codex from its session
logs, Claude Code through the `dagda statusline` hook described in the
[engine README](engine/README.md#plan-windows).

Accuracy has limits:

- Codex JSONL readers prefer structured per-response usage identities. Legacy
  turns without those records retain timing-based fork replay detection,
  disclosed in the report.
- Hermes uses per-call log timestamps only when usage fully reconciles with an
  unambiguous database row. Otherwise it preserves the aggregate, labels its
  dates and cost as approximate, and uses base context rates. Auxiliary tasks,
  ambiguous routes and incomplete logs remain aggregates. Log timestamps use
  the local timezone.
- Usage older than available price history uses the earliest known rate, with
  the affected cost disclosed.
- Hermes aggregates explicitly linked to a scanned Codex runtime thread are
  excluded from combined totals and retained in JSON `excluded_overlaps`. Mixed
  sessions and older histories without shared identities cannot be fully
  reconciled. See [runtime overlap rules](docs/provider-evidence-overlap.md).
- Missing, unreadable or changed source logs can make reports incomplete.
  Compressed Codex rollouts are not currently read.
- Cursor's local counters are best-effort and often empty. Automatic discovery
  reads measured counters and reports gaps, and missing cache details stay
  unpriced. An optional CSV replaces local history. See
  [Cursor setup](engine/README.md#cursor-usage).
- GitHub Copilot scans its already-written CLI session store and shutdown
  journal, plus VS Code's persisted chat-session records when they include
  complete per-model totals for a recognised GitHub Copilot participant. It
  never enables telemetry, and it does not retain or report prompt and response
  text. Valid request rows remain authoritative. Shutdown snapshots add only
  usage that reconciles against those rows in every token bucket, and ambiguous
  differences produce warnings. Older stores without measured counters remain
  unavailable. The Copilot CLI Agent Host's duplicate VS Code copy is excluded.
  An optional Copilot CLI OpenTelemetry file export fills conversations without
  native usage, and native records take precedence for overlapping
  conversations. See
  [export setup and limits](engine/README.md#optional-copilot-file-exports).
- Grok Build prefers its native `usage.json` ledger and uses legacy completion
  counters only when that ledger is absent. Missing timestamps and uncertain
  model attribution remain explicit and unpriced. Proven parent and child folds
  count once. Otherwise exact child history takes precedence over ambiguous
  parent aggregates, with a warning. Its subscription billing label is a
  normal-route default, not invoice attribution.
- Antigravity reads recognised invocation metadata from schema-gated local
  conversation databases. Unsupported database versions or layouts, malformed
  records and oversized metadata blobs are skipped, with no transcript-based
  token estimates. Numeric-only models remain visible but unpriced unless a
  pricing alias maps them. Its metadata has no endpoint or credential-mode
  field, so its subscription billing label is likewise a normal-route default,
  not invoice attribution.
- Tests cover specific storage formats and accounting cases, not every provider
  version or billing arrangement.

See [the command reference](engine/README.md) for filters, plans and pricing.
The [documentation index](docs/README.md) links provider setup, accounting
evidence and contributor guidance.

## Price updates

Models and prices update automatically when you run a report or `price`. The
CLI checks the dataset published by daily CI at most once per day, with a
three-second timeout and a cached or bundled fallback. No manual sync is
needed. Set `DAGDA_OFFLINE=1` to skip network checks. Your pricing overrides still
take priority. Updates preserve recorded rate history, and newly observed
changes take effect on the sync date. Paid-to-zero changes are retained at
their previous rates for review.

## Build from source

Requires Go 1.26 or newer.

```sh
make build
./dist/dagda -a
```

## Development

```sh
make test
make check
make build
make release TAG=v0.1.0  # archives and checksums for every platform in dist/release
make packages  # optional: needs Node.js, builds six native npm packages and a launcher
```

The Go module lives under `engine/`. Generated npm packages land in
`engine/dist/npm`, and this checkout does not establish that they have been
published. Releases are built and published locally, see
[development notes](docs/development.md#releases).

