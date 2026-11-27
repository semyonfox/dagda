package ingest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/semyonfox/dagda/engine/internal/model"
)

func TestCursorSDKResultsReconcileAndDeduplicate(t *testing.T) {
	root := t.TempDir()
	row := `{"id":"run-1","status":"finished","model":{"id":"m"},"usage":{"inputTokens":100,"outputTokens":50,"cacheReadTokens":700,"cacheWriteTokens":200,"totalTokens":1050,"reasoningTokens":20}}`
	writeFile(t, filepath.Join(root, "one.json"), row)
	writeFile(t, filepath.Join(root, "copy.json"), row)
	r, err := Run(context.Background(), []Scanner{&CursorAgent{root: root}})
	if err != nil || len(r.Errors) > 0 || len(r.Turns) != 1 || r.Duplicates != 1 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	u := r.Turns[0]
	if u.Usage.Input != 100 || u.Usage.Output != 50 || u.Usage.Reasoning != 20 || u.UnpricedReason == "" || !u.Timestamp.IsZero() {
		t.Fatalf("SDK net buckets or missing time mishandled: %+v", u)
	}
	t.Setenv("DAGDA_CURSOR_CSV", "")
	if !Selected(&CursorAgent{}, nil) || Selected(&VercelGateway{}, nil) {
		t.Fatal("SDK runs should be in default reports, gateway snapshots should not")
	}
	// the dashboard CSV bills SDK runs too
	csv := filepath.Join(root, "usage.csv")
	t.Setenv("DAGDA_CURSOR_CSV", csv)
	if !Selected(&CursorAgent{}, nil) {
		t.Fatal("a missing CSV must not hide SDK runs")
	}
	writeFile(t, csv, "Date,Model\n")
	if Selected(&CursorAgent{}, nil) || !Selected(&CursorAgent{}, []string{"cursor-agent"}) {
		t.Fatal("SDK runs must require selection while a Cursor CSV is configured")
	}
}

func TestCursorSDKSavedResultAcceptsGrossInput(t *testing.T) {
	root := t.TempDir()
	// 900 gross input with 800 cached leaves 100 fresh, and total is input+output
	writeFile(t, filepath.Join(root, "run.json"), `{"id":"run-2","status":"finished","createdAt":1795000000000,"model":{"id":"m"},"usage":{"inputTokens":900,"outputTokens":30,"cacheReadTokens":800,"cacheWriteTokens":0,"totalTokens":930}}`)
	turns := scan(t, &CursorAgent{root: root})
	if len(turns) != 1 || turns[0].Usage != (model.Usage{Input: 100, Output: 30, CacheRead: 800}) {
		t.Fatalf("gross saved result mishandled: %+v", turns)
	}
}

func TestCursorSDKRejectsDoubleCountedReasoning(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "bad.json"), `{"id":"run-1","status":"finished","usage":{"inputTokens":100,"outputTokens":50,"totalTokens":170,"reasoningTokens":20}}`)
	var n int
	err := (&CursorAgent{root: root}).Scan(context.Background(), func(model.Turn) { n++ })
	if err == nil || n != 0 {
		t.Fatalf("nonreconciling total accepted: %d %v", n, err)
	}
}
