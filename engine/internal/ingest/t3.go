package ingest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// t3Base is T3 Code's state dir, which T3 itself reads from T3CODE_HOME.
func t3Base(home string) string {
	if base := strings.TrimSpace(os.Getenv("T3CODE_HOME")); base != "" {
		return base
	}
	return filepath.Join(home, ".t3")
}

// t3Instance is the part of a T3 provider instance that decides where its
// agent keeps history. Only path fields are decoded because the settings file
// also holds api keys and server passwords.
type t3Instance struct {
	Driver string `json:"driver"`
	Config struct {
		HomePath  string `json:"homePath"`
		SetupMode string `json:"setupMode"`
	} `json:"config"`
	Environment []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"environment"`
}

// env returns the instance's own value for a variable. T3 lets the last
// definition win and matches names case-insensitively on windows.
func (i t3Instance) env(name string) string {
	value := ""
	for _, variable := range i.Environment {
		if variable.Name == name || runtime.GOOS == "windows" && strings.EqualFold(variable.Name, name) {
			value = strings.TrimSpace(variable.Value)
		}
	}
	return value
}

func t3Instances(home, driver string) []t3Instance {
	data, err := os.ReadFile(filepath.Join(t3Base(home), "userdata", "settings.json"))
	if err != nil {
		return nil
	}
	var settings struct {
		ProviderInstances map[string]t3Instance `json:"providerInstances"`
	}
	if json.Unmarshal(data, &settings) != nil {
		return nil
	}
	var instances []t3Instance
	for _, instance := range settings.ProviderInstances {
		if instance.Driver == driver {
			instances = append(instances, instance)
		}
	}
	return instances
}

// t3ClaudeHomes lists the Claude config dirs T3 Code instances run under. T3
// hands an instance's homePath, or else its own CLAUDE_CONFIG_DIR, to the
// agent sdk, so those transcripts never reach ~/.claude and the shell running
// dagda can't know about them. This mirrors T3's claudeUsageReader.
func t3ClaudeHomes(home string) []string {
	var homes []string
	for _, instance := range t3Instances(home, "claudeAgent") {
		dir := strings.TrimSpace(instance.Config.HomePath)
		if dir == "" {
			dir = instance.env("CLAUDE_CONFIG_DIR")
		}
		if dir != "" {
			homes = append(homes, expandHome(dir, home))
		}
	}
	slices.Sort(homes)
	return homes
}

// t3CodexHomes lists the shared CODEX_HOME of each T3 Code instance, mirroring
// T3's codexUsageReader: homePath, else the instance's CODEX_HOME unless T3
// manages the install. A shadowHomePath only overlays auth and symlinks
// sessions and archived_sessions back to the shared home, so it never holds
// rollouts of its own.
func t3CodexHomes(home string) []string {
	var homes []string
	for _, instance := range t3Instances(home, "codex") {
		dir := strings.TrimSpace(instance.Config.HomePath)
		if dir == "" && instance.Config.SetupMode != "managed" {
			dir = instance.env("CODEX_HOME")
		}
		if dir != "" {
			homes = append(homes, expandHome(dir, home))
		}
	}
	slices.Sort(homes)
	return homes
}

func expandHome(dir, home string) string {
	if dir == "~" || strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `~\`) {
		return filepath.Join(home, strings.TrimLeft(strings.TrimPrefix(dir, "~"), `/\`))
	}
	return dir
}
