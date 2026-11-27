package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/semyonfox/dagda/engine/internal/model"
)

type cursorSDKStoredUsage struct {
	Input     *int64 `json:"inputTokens"`
	Output    *int64 `json:"outputTokens"`
	Read      *int64 `json:"cacheReadTokens"`
	Write     *int64 `json:"cacheWriteTokens"`
	Total     *int64 `json:"totalTokens"`
	Reasoning *int64 `json:"reasoningTokens"`
}

func scanCursorAgentSDKStores(ctx context.Context, paths []string, emit func(model.Turn)) error {
	var scanErr error
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := scanCursorAgentSDKStore(ctx, path, emit); err != nil {
			scanErr = errors.Join(scanErr, fmt.Errorf("cursor-agent SDK store %q: %w", path, err))
		}
	}
	return scanErr
}

func cursorSDKStorePaths(ctx context.Context, root string) ([]string, error) {
	var paths []string
	var scanErr error
	projects, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	for _, project := range projects {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !project.IsDir() {
			continue
		}
		storeRoot := filepath.Join(root, project.Name(), "sdk-agent-store")
		states, err := os.ReadDir(storeRoot)
		if err != nil {
			if !os.IsNotExist(err) {
				scanErr = errors.Join(scanErr, err)
			}
			continue
		}
		for _, state := range states {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !state.IsDir() || !cursorSDKStateHash(state.Name()) {
				continue
			}
			indexPath := filepath.Join(storeRoot, state.Name(), "index.db")
			info, err := os.Lstat(indexPath)
			if err != nil {
				if !os.IsNotExist(err) {
					scanErr = errors.Join(scanErr, err)
				}
				continue
			}
			if info.Mode().IsRegular() {
				paths = append(paths, indexPath)
			}
		}
	}
	sort.Strings(paths)
	return paths, scanErr
}

// SDK 1.5 names the state dir sha256(workspaceRef) and renames older md5 dirs
// into place on first use, so both lengths can exist on one machine.
func cursorSDKStateHash(name string) bool {
	if len(name) != 32 && len(name) != 64 {
		return false
	}
	for _, r := range name {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

func scanCursorAgentSDKStore(ctx context.Context, path string, emit func(model.Turn)) error {
	db, err := sql.Open("sqlite", localSQLiteDSN(path))
	if err != nil {
		return err
	}
	defer db.Close()
	if err := requireLocalColumns(ctx, db, "runs", "run_id", "agent_id", "status", "model", "usage_json", "updated_at", "finished_at", "cancelled_at", "expired_at"); err != nil {
		return err
	}
	if err := requireLocalColumns(ctx, db, "agents", "agent_id", "workspace_ref"); err != nil {
		return err
	}
	// Project only native accounting and attribution columns. Checkpoints,
	// prompts, results, and event payloads can contain conversation content.
	rows, err := db.QueryContext(ctx, `SELECT
		r.run_id, r.agent_id, r.status, r.model, r.usage_json,
		a.workspace_ref, r.updated_at, r.finished_at, r.cancelled_at, r.expired_at
		FROM runs AS r JOIN agents AS a ON a.agent_id = r.agent_id
		ORDER BY r.run_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var scanErr error
	for rows.Next() {
		var runID, agentID, status, project, updated string
		var modelID, rawUsage, finished, cancelled, expired sql.NullString
		if err := rows.Scan(&runID, &agentID, &status, &modelID, &rawUsage, &project, &updated, &finished, &cancelled, &expired); err != nil {
			return err
		}
		switch status {
		case "FINISHED", "ERROR", "CANCELLED", "EXPIRED":
		default:
			continue
		}
		if runID == "" || agentID == "" {
			scanErr = errors.Join(scanErr, fmt.Errorf("terminal SDK row has empty run or agent id"))
			continue
		}
		if !rawUsage.Valid {
			// the SDK leaves usage unset when a run ends before any turn
			// reports it, which is normal for errored and cancelled runs
			if status != "FINISHED" {
				continue
			}
			scanErr = errors.Join(scanErr, fmt.Errorf("run %q: terminal SDK row has no usage_json", runID))
			continue
		}
		usage, err := parseCursorSDKStoredUsage(rawUsage.String)
		if err != nil {
			scanErr = errors.Join(scanErr, fmt.Errorf("run %q: %w", runID, err))
			continue
		}
		if usage.IsZero() {
			continue
		}

		// the run row keeps only the selected model. Subagents in observed
		// stores ran on the same model, so the run is priced at it as an
		// aggregate, which uses base rates rather than a context tier.
		id := modelID.String
		reason := ""
		if id == "" {
			id = "unknown"
			reason = "Cursor SDK run has no model selection"
		}
		rawTimestamp := cursorSDKTerminalTimestamp(status, updated, finished, cancelled, expired)
		timestamp := parseTime(rawTimestamp)
		if timestamp.IsZero() {
			reason = appendCursorAgentReason(reason, "Cursor SDK run has no valid terminal timestamp; historical pricing is unavailable")
			if rawTimestamp != "" {
				scanErr = errors.Join(scanErr, fmt.Errorf("run %q has invalid terminal timestamp %q", runID, rawTimestamp))
			}
		}
		emit(model.Turn{
			Key: identityKey("cursor-agent", runID), SessionID: agentID,
			Agent: model.Agent("cursor-agent"), Timestamp: timestamp, Model: id,
			Provider: "cursor", Project: project, Usage: usage, Aggregate: true,
			UnpricedReason: reason,
		})
	}
	return errors.Join(scanErr, rows.Err())
}

func parseCursorSDKStoredUsage(raw string) (model.Usage, error) {
	var stored cursorSDKStoredUsage
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return model.Usage{}, fmt.Errorf("invalid usage_json: %w", err)
	}
	if stored.Input == nil || stored.Output == nil || stored.Read == nil || stored.Write == nil || stored.Total == nil {
		return model.Usage{}, errors.New("usage_json is missing a required token bucket")
	}
	reasoning := int64(0)
	if stored.Reasoning != nil {
		reasoning = *stored.Reasoning
	}
	return cursorSDKUsage(*stored.Input, *stored.Output, *stored.Read, *stored.Write, *stored.Total, reasoning)
}

// cursorSDKUsage reconciles one SDK TokenUsage into disjoint buckets.
func cursorSDKUsage(input, output, read, write, total, reasoning int64) (model.Usage, error) {
	for _, count := range []int64{input, output, read, write, total, reasoning} {
		if count < 0 {
			return model.Usage{}, errors.New("usage contains a negative token count")
		}
	}
	usage, ok := (model.Usage{
		Input: input, Output: output, CacheRead: read, CacheWrite: write, Reasoning: reasoning,
	}).SanitizeAggregate()
	if !ok {
		return model.Usage{}, errors.New("usage token count exceeds aggregate bound")
	}
	// SDK 1.0 wrote totalTokens as the sum of four disjoint buckets. From 1.4
	// the runtime writes input+output and inputTokens includes the cached
	// prompt, so the cache buckets come out of input to keep them disjoint.
	switch cached := read + write; {
	case usage.Total() == total:
	case cached > 0 && input+output == total && input >= cached:
		usage.Input -= cached
	default:
		return model.Usage{}, fmt.Errorf("totalTokens %d matches neither disjoint buckets %d nor gross input plus output %d", total, usage.Total(), input+output)
	}
	if reasoning > output {
		return model.Usage{}, errors.New("reasoningTokens exceeds outputTokens")
	}
	return usage, nil
}

func cursorSDKTerminalTimestamp(status, updated string, finished, cancelled, expired sql.NullString) string {
	switch status {
	case "FINISHED":
		if finished.Valid {
			return finished.String
		}
	case "CANCELLED":
		if cancelled.Valid {
			return cancelled.String
		}
	case "EXPIRED":
		if expired.Valid {
			return expired.String
		}
	}
	return updated
}

func appendCursorAgentReason(current, next string) string {
	if current == "" {
		return next
	}
	if next == "" {
		return current
	}
	return current + "; " + next
}
