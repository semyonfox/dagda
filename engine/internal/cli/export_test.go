package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/semyonfox/dagda/engine/internal/report"
)

// isolatedMachine points every source and cache path at a fresh directory
// holding one Claude transcript.
func isolatedMachine(t *testing.T, lines ...string) {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "APPDATA", "LOCALAPPDATA", "T3CODE_HOME"} {
		t.Setenv(name, root)
	}
	t.Setenv("CLAUDE_CONFIG_DIRS", "")
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	t.Setenv("DAGDA_OFFLINE", "1")
	t.Setenv("DAGDA_PRICING_FILE", filepath.Join(root, "missing-pricing.json"))
	t.Setenv("DAGDA_PLANS_FILE", filepath.Join(root, "missing-plans.json"))
	path := filepath.Join(root, "projects", "fixture", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A session copied to another machine shares its message and request ids, so
// merging that machine's export must count the copied call once.
func TestImportDropsCallsCopiedBetweenMachines(t *testing.T) {
	const copied = `{"type":"assistant","timestamp":"2026-11-05T10:00:00Z","requestId":"r1","sessionId":"s1","message":{"id":"m1","model":"fixture","usage":{"input_tokens":100}}}`
	isolatedMachine(t, copied,
		`{"type":"assistant","timestamp":"2026-11-06T10:00:00Z","requestId":"r3","sessionId":"s2","message":{"id":"m3","model":"fixture","usage":{"input_tokens":300}}}`,
		`{"type":"assistant","timestamp":"2026-11-06T11:00:00Z","sessionId":"s2","message":{"model":"fixture","usage":{"input_tokens":50}}}`)
	exported := filepath.Join(t.TempDir(), "laptop.dagda")
	code, stdout, stderr := runCapturedCLI(t, "export", "--agent", "claude", "--output", exported)
	if code != 0 || stderr != "" || stdout != "" {
		t.Fatalf("export exit=%d stdout=%d bytes stderr=%s", code, len(stdout), stderr)
	}

	isolatedMachine(t, copied,
		`{"type":"assistant","timestamp":"2026-11-05T11:00:00Z","requestId":"r2","sessionId":"s1","message":{"id":"m2","model":"fixture","usage":{"input_tokens":200}}}`)
	code, out, stderr := runCapturedCLI(t, "-a", "--agent", "claude", "--json", "--import", exported)
	if code != 0 || stderr != "" {
		t.Fatalf("report exit=%d stderr=%s", code, stderr)
	}
	var rep report.Report
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	// 200 + 100 here, 300 + 50 from the export, the copy of the 100 dropped
	if rep.Totals.Usage.Total() != 650 || rep.Totals.Turns != 4 {
		t.Fatalf("totals=%+v, want 650 tokens across 4 turns", rep.Totals)
	}
	if rep.ImportedDuplicates != 1 || rep.ImportedUnkeyed != 1 || rep.Duplicates != 0 {
		t.Fatalf("imported duplicates=%d unkeyed=%d local duplicates=%d, want 1, 1 and 0",
			rep.ImportedDuplicates, rep.ImportedUnkeyed, rep.Duplicates)
	}

	// a report is not an export, and is refused rather than merged as zero records
	notExport := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(notExport, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runCapturedCLI(t, "-a", "--json", "--import", notExport); code != 1 || !strings.Contains(stderr, "not a dagda export") {
		t.Fatalf("exit=%d stderr=%s, want a rejected import", code, stderr)
	}
}
