package ingest

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func legacyMessage(ts, metrics string) string {
	return `{"type":"say","say":"api_req_started","ts":` + ts + `,"text":` + strconv.Quote(metrics) + `}`
}

func writeLegacyClineTask(t *testing.T, root, taskID string, messages ...string) {
	t.Helper()
	dir := filepath.Join(root, "tasks", taskID)
	writeFile(t, filepath.Join(dir, "ui_messages.json"), `[`+strings.Join(messages, ",")+`]`)
}

func writeLegacyClineHistory(t *testing.T, root, taskID, text string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "tasks", taskID, "api_conversation_history.json"),
		`[{"role":"user","content":[{"type":"text","text":`+strconv.Quote(text)+`}]}]`)
}

func TestLegacyClineKeepsDisjointInputAndHistoryMetadata(t *testing.T) {
	root := t.TempDir()
	writeLegacyClineTask(t, root, "task-1",
		`{"type":"say","say":"text","ts":1788307000000,"text":"ignored"}`,
		legacyMessage("1788307200000", `{"tokensIn":300,"tokensOut":40,"cacheReads":600,"cacheWrites":100,"cost":0.01}`),
		legacyMessage(`"2026-09-02T00:01:00Z"`, `{"tokensIn":200,"tokensOut":20}`),
	)
	writeLegacyClineHistory(t, root, "task-1", "<model>openrouter/author/model</model>\nCurrent Workspace Directory (/example/project)")

	turns := scan(t, newClineAt(root))
	if len(turns) != 2 {
		t.Fatalf("turns = %d: %+v", len(turns), turns)
	}
	if turns[0].Usage.Input != 300 || turns[0].Usage.ContextTokens != 1000 || turns[0].Usage.Total() != 1040 {
		t.Fatalf("classic net input was changed: %+v", turns[0].Usage)
	}
	if turns[0].Model != "openrouter/author/model" || turns[0].Project != "/example/project" || turns[0].SessionID != "task-1" {
		t.Fatalf("metadata lost: %+v", turns[0])
	}
	if turns[1].Timestamp.IsZero() || turns[0].Agent != "cline" {
		t.Fatalf("source attribution lost: %+v", turns)
	}
}

func TestLegacyClineUnknownModelStaysExplicitAndMalformedRowsDoNotAbort(t *testing.T) {
	root := t.TempDir()
	writeLegacyClineTask(t, root, "task",
		legacyMessage(`"not-a-time"`, `{broken`),
		legacyMessage(`"not-a-time"`, `{"tokensIn":10,"tokensOut":2}`),
		legacyMessage("1788307200002", `{"tokensIn":0,"tokensOut":0}`),
	)
	turns := scan(t, newClineAt(root))
	if len(turns) != 1 || turns[0].Model != "unknown" || !turns[0].Timestamp.IsZero() {
		t.Fatalf("unexpected fallback: %+v", turns)
	}
}

func TestLegacyClineDedupsSameTaskAcrossEditorStores(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	message := legacyMessage("1788307200000", `{"tokensIn":10,"tokensOut":2}`)
	writeLegacyClineTask(t, a, "task", message)
	writeLegacyClineTask(t, b, "task", message)
	result, err := Run(context.Background(), []Scanner{newClineAt(a, b)})
	if err != nil || len(result.Errors) != 0 {
		t.Fatalf("scan: %v %v", err, result.Errors)
	}
	if len(result.Turns) != 1 || result.Duplicates != 1 {
		t.Fatalf("duplicate task copy counted: %+v", result)
	}
}

// Extensions running over Remote SSH, WSL or a container keep their data under
// the editor's server on the remote side, and forks keep it under their own
// name. Copilot only reads VS Code builds, whose chat store is Copilot's.
func TestVSCodeExtensionStoresAreFoundInServersAndForks(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "APPDATA"} {
		t.Setenv(name, filepath.Join(home, "unused-"+name))
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLINE_DATA_DIR", filepath.Join(home, "no-cline-cli"))
	remote := filepath.Join(home, ".vscode-server", "data", "User")
	fork := filepath.Join(home, ".cursor-server", "data", "User")
	for _, user := range []string{remote, fork} {
		writeFile(t, filepath.Join(user, "globalStorage", clineExtensionID, "tasks", "t", "ui_messages.json"), "[]")
		writeFile(t, filepath.Join(user, "workspaceStorage", "w", "chatSessions", "s.json"), "{}")
	}
	got := NewCline().Roots()
	want := []string{filepath.Join(remote, "globalStorage", clineExtensionID), filepath.Join(fork, "globalStorage", clineExtensionID)}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("cline roots = %q, want %q", got, want)
	}
	if got := NewCopilot().Roots(); len(got) != 1 || got[0] != remote {
		t.Fatalf("copilot roots = %q, want only %q", got, remote)
	}
}
