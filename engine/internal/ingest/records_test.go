package ingest

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
)

// Two machines with different home dirs. A Codebuff chat with no message id is
// keyed by its path, so a copy has to match by its place under the home dir.
// A LingTai ledger row with no call id is keyed by path and line, which two
// machines can share for unrelated calls, so it must never match even at the
// same absolute path.
func TestExportMatchesCopiesAcrossHomesButNotPositionalKeys(t *testing.T) {
	chat := filepath.Join(".config", "manicode", "projects", "p", "chats", "chat-1", "chat-messages.json")
	lingtai := t.TempDir()
	machine := func(lingtaiInput string) []Scanner {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		writeFile(t, filepath.Join(home, chat), `[{"variant":"assistant","timestamp":"2026-09-01T00:00:00Z","metadata":{"model":"m","usage":{"inputTokens":7}}}]`)
		writeFile(t, filepath.Join(lingtai, "agent", "logs", "token_ledger.jsonl"), `{"ts":"2026-09-01T00:00:00Z","input":`+lingtaiInput+`,"model":"m"}`)
		return []Scanner{newCodebuffAt(filepath.Join(home, ".config", "manicode")), newLingTaiAt(lingtai)}
	}

	laptop, err := Run(context.Background(), machine("3"))
	if err != nil {
		t.Fatal(err)
	}
	var export bytes.Buffer
	if err := WriteRecords(&export, laptop.Turns); err != nil {
		t.Fatal(err)
	}
	server, err := Run(context.Background(), machine("5"))
	if err != nil {
		t.Fatal(err)
	}
	imported, sameReaders, err := ReadRecords(&export)
	if err != nil || !sameReaders {
		t.Fatalf("read: %v sameReaders=%v", err, sameReaders)
	}
	server.Import(imported)

	// 7 once for the copied chat, 5 and 3 for the two unrelated ledger rows
	var input int64
	for _, turn := range server.Turns {
		input += turn.Usage.Input
	}
	if input != 15 || server.ImportedDuplicates != 1 || server.ImportedUnkeyed != 1 {
		t.Fatalf("input=%d imported duplicates=%d unkeyed=%d, want 15, 1 and 1", input, server.ImportedDuplicates, server.ImportedUnkeyed)
	}
}

func TestReadRecordsRejectsOtherFiles(t *testing.T) {
	for _, data := range []string{"", `{"format":"dagda-records","version":1,"turns":[]}`} {
		if _, _, err := ReadRecords(bytes.NewBufferString(data)); err == nil {
			t.Fatalf("accepted %q", data)
		}
	}
}
