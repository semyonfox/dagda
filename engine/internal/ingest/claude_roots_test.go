package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeMultipleRootsAndNestedArchives(t *testing.T) {
	base := t.TempDir()
	native := filepath.Join(base, "native")
	archive := filepath.Join(base, "archive")
	replayed := claudeLine("message-a", "request-a", "claude-sonnet-4", "2026-09-01T10:00:00Z", 100, 30, 200, 20)
	writeFile(t, filepath.Join(native, "projects", "project", "original.jsonl"), replayed)
	writeFile(t, filepath.Join(archive, "device", "claude", "project", "copy.jsonl"), replayed)
	writeFile(t, filepath.Join(archive, "device", "claude", "project", "session", "subagents", "child.jsonl"),
		claudeLine("message-b", "request-b", "claude-sonnet-4", "2026-09-01T11:00:00Z", 50, 40, 80, 10))
	t.Setenv("CLAUDE_CONFIG_DIRS", native+","+archive+","+filepath.Join(native, "projects"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(base, "ignored"))
	s := NewClaude()
	if got := len(s.Roots()); got != 2 {
		t.Fatalf("roots = %v, want two distinct roots", s.Roots())
	}
	r, err := Run(context.Background(), []Scanner{s})
	if err != nil || len(r.Turns) != 2 || r.Duplicates != 1 {
		t.Fatalf("result = %+v, err = %v", r, err)
	}
	if !r.Turns[1].Subagent || r.Turns[1].Project != "/proj" || totalOut(r.Turns) != 70 {
		t.Fatalf("attribution or usage lost: %+v", r.Turns)
	}
}

func TestClaudeDefaultXDGAndLegacyRoots(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, "custom-config")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("CLAUDE_CONFIG_DIRS", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("T3CODE_HOME", filepath.Join(home, "no-t3"))
	for _, dir := range []string{filepath.Join(xdg, "claude", "projects"), filepath.Join(home, ".claude", "projects")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if roots := NewClaude().Roots(); len(roots) != 2 {
		t.Fatalf("roots = %v, want XDG and legacy", roots)
	}
	// A missing explicit location must not cause an unrelated default scan.
	t.Setenv("CLAUDE_CONFIG_DIRS", filepath.Join(home, "missing"))
	if roots := NewClaude().Roots(); len(roots) != 0 {
		t.Fatalf("missing override fell back to %v", roots)
	}
}

func TestClaudeFollowsT3CodeInstanceHomes(t *testing.T) {
	home := t.TempDir()
	t3 := filepath.Join(home, "t3")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CLAUDE_CONFIG_DIRS", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("T3CODE_HOME", t3)
	work := filepath.Join(home, "claude-work")
	viaEnv := filepath.Join(home, "claude-env")
	codex := filepath.Join(home, "codex-home")
	writeFile(t, filepath.Join(t3, "userdata", "settings.json"), `{"providerInstances":{
		"claudeAgent":{"driver":"claudeAgent","config":{"homePath":"~/claude-work"}},
		"claudeWork":{"driver":"claudeAgent","config":{"homePath":""},"environment":[{"name":"CLAUDE_CONFIG_DIR","value":"`+filepath.ToSlash(viaEnv)+`"}]},
		"codex":{"driver":"codex","config":{"homePath":"`+filepath.ToSlash(codex)+`"}}}}`)
	writeFile(t, filepath.Join(work, "projects", "p", "a.jsonl"),
		claudeLine("message-a", "request-a", "claude-sonnet-4", "2026-09-01T10:00:00Z", 100, 30, 0, 0))
	writeFile(t, filepath.Join(viaEnv, "projects", "p", "b.jsonl"),
		claudeLine("message-b", "request-b", "claude-sonnet-4", "2026-09-01T11:00:00Z", 50, 40, 0, 0))
	writeFile(t, filepath.Join(codex, "projects", "p", "c.jsonl"),
		claudeLine("message-c", "request-c", "claude-sonnet-4", "2026-09-01T12:00:00Z", 1, 1, 0, 0))
	r, err := Run(context.Background(), []Scanner{NewClaude()})
	if err != nil || len(r.Turns) != 2 || totalOut(r.Turns) != 70 {
		t.Fatalf("want both t3 claude homes and not the codex one, got %+v, err = %v", r.Turns, err)
	}
	// an explicit override still means exactly those dirs
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "missing"))
	if roots := NewClaude().Roots(); len(roots) != 0 {
		t.Fatalf("explicit override picked up t3 homes: %v", roots)
	}
}

func TestCodexFollowsT3CodeInstanceHomes(t *testing.T) {
	home := t.TempDir()
	t3 := filepath.Join(home, "t3")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("T3CODE_HOME", t3)
	work := filepath.Join(home, "codex-work")
	viaEnv := filepath.Join(home, "codex-env")
	managed := filepath.Join(home, "codex-managed-env")
	writeFile(t, filepath.Join(t3, "userdata", "settings.json"), `{"providerInstances":{
		"codex":{"driver":"codex","config":{"homePath":"~/codex-work","shadowHomePath":"~/codex-shadow"}},
		"codexEnv":{"driver":"codex","config":{},"environment":[{"name":"CODEX_HOME","value":"`+filepath.ToSlash(viaEnv)+`"}]},
		"codexManaged":{"driver":"codex","config":{"setupMode":"managed"},"environment":[{"name":"CODEX_HOME","value":"`+filepath.ToSlash(managed)+`"}]},
		"claudeAgent":{"driver":"claudeAgent","config":{"homePath":"~/.codex"}}}}`)
	for _, dir := range []string{
		filepath.Join(home, ".codex", "sessions"),
		filepath.Join(work, "sessions"),
		filepath.Join(work, "archived_sessions"),
		filepath.Join(viaEnv, "sessions"),
		filepath.Join(managed, "sessions"),
		filepath.Join(home, "codex-shadow", "sessions"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// the default home, both of the first instance's dirs and the env home.
	// the managed instance ignores its CODEX_HOME and the shadow is skipped
	if roots := NewCodex().Roots(); len(roots) != 4 {
		t.Fatalf("roots = %v", roots)
	}
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	if roots := NewCodex().Roots(); len(roots) != 1 {
		t.Fatalf("explicit CODEX_HOME picked up t3 homes: %v", roots)
	}
}
