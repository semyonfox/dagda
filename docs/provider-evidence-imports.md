# Imports and additional readers

Sources checked on 19 September 2026. These implementations do not start agents,
install hooks, read credentials, or request account data over the network.

## Cursor Agent SDK

This local-store follow-up was checked on 20 September 2026 and updated on 27
November 2026 against `@cursor/sdk` 1.0.38, 1.4.2 and 1.5.0 as bundled by T3
Code, which drives Cursor through the SDK's local runtime.

With `DAGDA_CURSOR_AGENT_DIR` unset, dagda discovers the SDK's default SQLite
stores at `~/.cursor/projects/<workspace-slug>/sdk-agent-store/<hash>/index.db`.
The SDK resolves that path from the home directory on every platform, so it is
`%USERPROFILE%\.cursor\projects` on Windows. SDK 1.5 names the state
directory `sha256(workspaceRef)` (64 hex characters) and renames an older
md5-named directory (32 characters) into place on first use. Both are accepted.
It reads only terminal `runs` joined to `agents`, using `run_id`, `agent_id`,
`status`, `model`, `usage_json`, `workspace_ref` and terminal timestamps. The
databases use WAL mode, so they are opened read-only without hiding the live
WAL. A copied store is deduplicated by the native run ID. An ordinary Cursor
project directory without an SDK `index.db` does not count as an installed
source.

The first contract check used the official
[`@cursor/sdk` 1.0.31 npm artifact](https://registry.npmjs.org/@cursor/sdk/-/sdk-1.0.31.tgz)
(npm integrity
`sha512-0SdJQqp5oXn81oJqIVkLpgHih+CL6CAudK83pCsdJyA23AvInSWlMaGpLi+JlrK3efHoswiEhhASs9WpeEz3QQ==`,
tarball SHA-256
`6316337f3bf154406d704f00c939031ebaa388031ebfcabde843f74452c02a03`).
Its bundled `cursor-sdk-local-runtime/dist/run-store/sqlite-agent-run-store.js`
defines the path, schema, terminal statuses, WAL configuration and persistence
of `usage_json`.

The token buckets changed shape after that. In 1.0.38 `toTokenUsage` writes
`totalTokens` as input plus output plus both cache buckets. In 1.4.2 and 1.5.0
it writes `totalTokens: inputTokens + outputTokens`, and `inputTokens` is the
gross prompt including cached tokens. Cursor's
[SDK token-usage page](https://cursor.com/docs/sdk/typescript#token-usage) still
documents the older four-bucket total. Every run recorded on the maintainer's
machine by 1.5.0 has `inputTokens >= cacheReadTokens + cacheWriteTokens` and
`totalTokens == inputTokens + outputTokens`. The reader accepts either shape: a
four-bucket total is used as is, and a gross total has the cache buckets
subtracted from input so the four buckets stay disjoint. Anything else is
reported as invalid. Reasoning stays a subset of output.

Runs that end before any turn reports usage have no `usage_json`, which the SDK
documents for cancelled runs. Errored, cancelled and expired rows without usage
are skipped quietly. A finished run without usage is still reported.

The persisted `model` is the run's selected model. Subagent tool calls in the
recorded stores used the same model as their parent run, so each run is priced
at its selected model as an aggregate, at base rates with no long-context tier.
A run with no model, or a routed `auto` or `default` selection whose real model
the store never records, stays unpriced. Local stores contain no billed cost. Dagda
does not call the authenticated SDK usage endpoint, read credentials or query
Cursor's backend.

The SDK store is part of default reports. The IDE database cannot hold the same
runs: SDK agents and runs use `agent-<uuid>` and `run-<uuid>` ids from a separate
in-process runtime, while IDE composers use plain UUIDs in `state.vscdb`. The
dashboard CSV does bill SDK runs, so while `DAGDA_CURSOR_CSV` points at a file the
SDK source needs `--agent cursor-agent`, and combining it with `--agent cursor`
is rejected.

The other headless stores under `~/.cursor` carry no usage. `chats/*/store.db`
(CLI) and `acp-sessions/*/store.db` (`agent acp`) are content-addressed message
blobs with no token fields outside message text, `ai-tracking` counts code
lines, and `agent-cli-state.json` holds UI flags. This store belongs to SDK
applications. A configured `JsonlLocalAgentStore` can live at an arbitrary
application path and cannot be discovered safely.

Setting `DAGDA_CURSOR_AGENT_DIR` overrides automatic discovery and retains the
older importer for caller-saved native SDK `RunResult` JSON files. Bare results
without time or conversation identity stay undated. The importer never invents
either value from the filename.

## Mux / Xum

Read native `sessions/**/chat.jsonl`, including subagent transcripts and separately
billed `toolModelUsages`. Ignore overlapping usage sidecars and analytics DBs.
`XUM_ROOT` takes precedence over `MUX_ROOT`; default discovery includes `.xum`,
`.mux`, and `.cmux`. The provider identifier remains `mux` for compatibility.

The [persisted metadata](https://github.com/coder/mux/blob/e12de4556a23c3e670fb0559062686ff1c88aaf5/src/common/types/message.ts)
sums multiple API steps into one assistant record. Records are therefore marked
as aggregates. The [normalizer](https://github.com/coder/mux/blob/e12de4556a23c3e670fb0559062686ff1c88aaf5/src/common/utils/tokens/usageHelpers.ts)
and [display accounting](https://github.com/coder/mux/blob/e12de4556a23c3e670fb0559062686ff1c88aaf5/src/common/utils/tokens/displayUsage.ts)
establish SDK 6+ gross input and reasoning-inclusive output. Inconsistent legacy
buckets produce a diagnostic rather than guessed accounting. Native IDs deduplicate
copied stores. Older SDK 5 rows without a version marker remain a compatibility limit.

## Zerostack

Read `$ZS_DATA_DIR/sessions`, otherwise the platform data directory under
`zerostack/sessions`. The [session type](https://github.com/gi-dellav/zerostack/blob/9744dbda838c66958a31f253077afcb24cbea304/src/session/mod.rs)
now includes cached and cache-creation totals. Its
[pricing code](https://github.com/gi-dellav/zerostack/blob/9744dbda838c66958a31f253077afcb24cbea304/src/pricing.rs)
documents native Anthropic input as net and other built-in protocols as gross.

Only session totals survive. The reader retains the final model/protocol tag,
labels the aggregate, and excludes its cost because historical switches cannot
be reconstructed. For current built-in routes it decomposes using the final
protocol and explicitly warns of that limitation. Old/custom-route raw input
remains unclassified and can omit native Anthropic cache usage. It is not an
invoice reconciliation source.

## Quick Desktop

Read legacy `$QUICKWORK_HOME/metrics/metrics-YYYY-MM-DD.jsonl` and profile data
paths from `profiles.json`. Default home is `.quickwork`. Only numeric `Model`,
`InputTokens`, `OutputTokens`, and `_aws.Timestamp` records qualify. No database
text estimates are used. Missing cache semantics leave input unclassified and
cost excluded. Identical copies retain the maximum observed multiplicity of
identical rows; independent identical events without IDs cannot be distinguished.

[AWS's current desktop documentation](https://docs.aws.amazon.com/quick/latest/userguide/desktop-security.html)
describes cloud-backed conversation storage and does not publish the old local
metrics contract. This reader is explicitly legacy observed-schema support,
not a guarantee of current Quick cloud coverage.

## Antigravity

By default the reader discovers native version-1 conversation databases below
the Antigravity CLI, IDE, backup and configuration roots, plus the per-instance
profiles T3 Code creates at
`<T3CODE_HOME or ~/.t3>/userdata/providers/antigravity/<sha256>/antigravity-acp`.
T3 runs never write to `~/.gemini`, and the two sets of conversation ids are
disjoint. It reads only
`gen_metadata.data` and timestamp linkage from `steps.metadata`. Metadata is
bounded at 64 MiB and protobuf parsing retains only the direct usage, model,
response identity and timestamp field paths. Numeric private model IDs remain
explicitly unpriced. Copied native response IDs deduplicate across stores.

Set `DAGDA_ANTIGRAVITY_DIR` to replace native discovery with a directory of saved
`stream-json` captures
(`.jsonl` or `.ndjson`). The [headless output contract](https://www.antigravity.google/docs/cli/headless/)
defines one `result` per user turn, but its usage counters are cumulative over
the conversation. One conversation identity retains the most complete snapshot
across turns and copied captures; steps are not added again. Conflicting
cumulative buckets or counters that reset while turn numbers advance produce a
diagnostic and are excluded.
Unfinished turns without a result produce a diagnostic and are excluded.

An explicit model override can appear in `init`, but it does not establish every
historical request model. Public output lacks wall-clock timestamps. Captures remain
undated, unpriced aggregates; use an all-history command such as `daily --agent
antigravity --plain` to see them. This does not install a capture hook or claim
coverage of statusline history. Native and imported ledgers are deliberately
not scanned together because they may contain the same calls.

## Vercel AI Gateway

Set `DAGDA_VERCEL_REPORT` to one saved `/v1/report` JSON response and select
`--agent vercel-gateway`. Replace the file for a refreshed snapshot. There is no
automatic credential discovery or live request. Gateway totals may overlap
native agent logs, so this reader requires explicit selection.

The [current API contract](https://vercel.com/docs/ai-gateway/observability-and-spend/custom-reporting)
allows **one grouping dimension**. `group_by=model` does not return daily model
rows. Accept model, day, or hour grouping;
reject unsupported/mixed dimensions and repeated buckets. Model grouping stays
undated; day grouping has an unknown model. Cache inclusion is not documented
well enough to price the raw input count, so it remains unclassified. Charged
`total_cost`, market value, and BYOK zeros are not substituted for API list value.

## OpenCode and Claude discovery updates

OpenCode's [current native schema](https://github.com/anomalyco/opencode/blob/dev/packages/core/src/session/sql.ts) stores assistant records in `session_message`, joined to `session`. The transitional `session_v2` layout and frozen legacy `message` rows are also supported. Modern rows mask matching legacy IDs; distinct pre-migration history remains available. Native `OPENCODE_DB`, channel database names and explicit `OPENCODE_DATA_DIR` discovery are supported. Tests cover coexistence and precedence with synthetic SQLite fixtures.

Claude discovery now supports multiple `CLAUDE_CONFIG_DIRS`, singular `CLAUDE_CONFIG_DIR`, XDG and legacy roots, plus recursive archives and subagent logs. Canonical paths avoid scanning the same directory twice, while native request IDs deduplicate copied records. Plural configuration takes precedence over singular configuration. These environment/discovery additions do not change Claude's per-request accounting.
