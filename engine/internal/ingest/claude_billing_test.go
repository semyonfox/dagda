package ingest

import (
	"context"
	"path/filepath"
	"testing"
)

// the usage block is Anthropic's own compaction example: the top-level counts
// match the message step and leave the compaction step out
const claudeCompactedCall = `{"type":"assistant","timestamp":"2026-10-01T10:00:00Z","requestId":"req_1","sessionId":"s","message":{"id":"msg_1","model":"claude-opus-5","usage":{"input_tokens":23000,"output_tokens":1000,"speed":"fast","iterations":[{"type":"compaction","input_tokens":180000,"output_tokens":3500},{"type":"message","input_tokens":23000,"output_tokens":1000}]}}}`

func TestClaudeCountsCompactionStepsAndFastMode(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "projects", "-proj")
	writeFile(t, filepath.Join(proj, "a.jsonl"), claudeCompactedCall)
	// a resumed transcript replays the call, compaction step included
	writeFile(t, filepath.Join(proj, "b.jsonl"), claudeCompactedCall)

	res, err := Run(context.Background(), []Scanner{newClaudeAt(root)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Turns) != 2 || res.Duplicates != 2 {
		t.Fatalf("want 2 turns and 2 duplicates, got %d and %d: %+v", len(res.Turns), res.Duplicates, res.Turns)
	}
	var input, output int64
	for _, turn := range res.Turns {
		input += turn.Usage.Input
		output += turn.Usage.Output
		if turn.Model != "claude-opus-5-fast" {
			t.Fatalf("fast mode must price at the -fast rates: %q", turn.Model)
		}
	}
	// 180000 + 23000 and 3500 + 1000, the documented billed totals
	if input != 203000 || output != 4500 {
		t.Fatalf("billed input %d output %d", input, output)
	}
}

func TestClaudeCountsCompactionOnlyCall(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "projects", "-proj", "a.jsonl"),
		`{"type":"assistant","timestamp":"2026-10-01T10:00:00Z","requestId":"req_1","sessionId":"s","message":{"id":"msg_1","model":"claude-opus-5","usage":{"input_tokens":0,"output_tokens":0,"iterations":[{"type":"compaction","input_tokens":144,"output_tokens":276}]}}}`)
	res, err := Run(context.Background(), []Scanner{newClaudeAt(root)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Turns) != 1 || res.Turns[0].Usage.Input != 144 || res.Turns[0].Usage.Output != 276 {
		t.Fatalf("compaction-only call: %+v", res.Turns)
	}
}

func TestClaudeCountsAdvisorStepsAtTheirOwnModel(t *testing.T) {
	root := t.TempDir()
	call := `{"type":"assistant","timestamp":"2026-10-01T10:00:00Z","requestId":"req_1","sessionId":"s","message":{"id":"msg_1","model":"claude-sonnet-5","usage":{"input_tokens":400,"output_tokens":50,"iterations":[{"type":"message","input_tokens":300,"output_tokens":20},{"type":"advisor_message","model":"claude-opus-5","input_tokens":1200,"cache_read_input_tokens":800,"output_tokens":90},{"type":"advisor_message","model":"<synthetic>","input_tokens":7,"output_tokens":7},{"type":"message","input_tokens":100,"output_tokens":30}]}}}`
	writeFile(t, filepath.Join(root, "projects", "-proj", "a.jsonl"), call)
	// a resumed transcript replays the advisor step too
	writeFile(t, filepath.Join(root, "projects", "-proj", "b.jsonl"), call)
	res, err := Run(context.Background(), []Scanner{newClaudeAt(root)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Turns) != 2 || res.Duplicates != 2 {
		t.Fatalf("want executor and advisor turns once each, got %+v (dups %d)", res.Turns, res.Duplicates)
	}
	byModel := map[string]int64{}
	for _, turn := range res.Turns {
		byModel[turn.Model] += turn.Usage.Input + turn.Usage.CacheRead + turn.Usage.Output
	}
	// executor 400 + 50, advisor 1200 + 800 + 90
	if byModel["claude-sonnet-5"] != 450 || byModel["claude-opus-5"] != 2090 {
		t.Fatalf("usage by model: %v", byModel)
	}
}

func TestClaudeFastModeSurvivesSnapshotOrder(t *testing.T) {
	root := t.TempDir()
	early := `{"type":"assistant","timestamp":"2026-10-01T10:00:00Z","requestId":"req_1","sessionId":"s","message":{"id":"msg_1","model":"claude-opus-5","usage":{"input_tokens":5,"output_tokens":1}}}`
	final := `{"type":"assistant","timestamp":"2026-10-01T10:00:02Z","requestId":"req_1","sessionId":"s","message":{"id":"msg_1","model":"claude-opus-5","usage":{"input_tokens":5,"output_tokens":90,"speed":"fast"}}}`
	writeFile(t, filepath.Join(root, "projects", "-proj", "a.jsonl"), early, final)
	res, err := Run(context.Background(), []Scanner{newClaudeAt(root)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Turns) != 1 || res.Turns[0].Model != "claude-opus-5-fast" || res.Turns[0].Usage.Output != 90 {
		t.Fatalf("snapshots: %+v", res.Turns)
	}
}

func TestClaudeBatchTierSurvivesSnapshotOrder(t *testing.T) {
	root := t.TempDir()
	early := `{"type":"assistant","timestamp":"2026-10-01T10:00:00Z","requestId":"req_1","sessionId":"s","message":{"id":"msg_1","model":"claude-opus-5","usage":{"input_tokens":5,"output_tokens":1}}}`
	full := `{"type":"assistant","timestamp":"2026-10-01T10:00:02Z","requestId":"req_1","sessionId":"s","message":{"id":"msg_1","model":"claude-opus-5","usage":{"input_tokens":5,"output_tokens":90,"service_tier":"batch"}}}`
	laterSmall := `{"type":"assistant","timestamp":"2026-10-01T10:00:03Z","requestId":"req_2","sessionId":"s","message":{"id":"msg_2","model":"claude-opus-5","usage":{"input_tokens":5,"output_tokens":1,"service_tier":"batch"}}}`
	earlyLarge := `{"type":"assistant","timestamp":"2026-10-01T10:00:01Z","requestId":"req_2","sessionId":"s","message":{"id":"msg_2","model":"claude-opus-5","usage":{"input_tokens":5,"output_tokens":90}}}`
	writeFile(t, filepath.Join(root, "projects", "-proj", "a.jsonl"), early, full, earlyLarge, laterSmall)
	res, err := Run(context.Background(), []Scanner{newClaudeAt(root)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Turns) != 2 {
		t.Fatalf("turns: %+v", res.Turns)
	}
	for _, turn := range res.Turns {
		if turn.ServiceTier != "batch" || turn.Usage.Output != 90 {
			t.Fatalf("snapshot lost batch tier or usage: %+v", turn)
		}
	}
}
