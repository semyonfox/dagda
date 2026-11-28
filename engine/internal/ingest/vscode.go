package ingest

import (
	"os"
	"path/filepath"
	"runtime"
)

// vscodeEditor is one VS Code build or fork: its folder in the user config
// dir and the server it installs over SSH, WSL and containers. Extensions
// running remotely keep their data on the remote side, under the server.
type vscodeEditor struct {
	product, server string
	fork            bool
}

var vscodeEditors = []vscodeEditor{
	{product: "Code", server: ".vscode-server"},
	{product: "Code - Insiders", server: ".vscode-server-insiders"},
	{product: "Code - OSS"},
	{product: "VSCodium", server: ".vscodium-server"},
	// Codespaces and dev containers
	{server: ".vscode-remote"},
	{product: "Cursor", server: ".cursor-server", fork: true},
	{product: "Windsurf", server: ".windsurf-server", fork: true},
	{product: "Trae", server: ".trae-server", fork: true},
}

// flatpakVSCode pairs Flathub app ids with the product folder inside their
// sandboxed config dir. A slice, not a map, so roots keep a stable order and
// the scan cache keeps hitting.
var flatpakVSCode = [][2]string{
	{"com.visualstudio.code", "Code"},
	{"com.visualstudio.code-insiders", "Code - Insiders"},
	{"com.vscodium.codium", "VSCodium"},
}

// vscodeUserDirs lists every VS Code style User directory that exists. Forks
// replace VS Code's own chat with theirs, so only extension stores, which are
// named by extension id, are read from them.
func vscodeUserDirs(forks bool) []string {
	config, _ := os.UserConfigDir()
	var dirs []string
	for _, dir := range vscodeUserDirsFor(runtime.GOOS, homeDir(), config, forks) {
		if existingDir(dir) != "" {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// vscodeUserDirsFor is the candidate list, without touching the disk. config
// is the platform's user config dir, which on Linux honours XDG_CONFIG_HOME.
func vscodeUserDirsFor(goos, home, config string, forks bool) []string {
	var dirs []string
	add := func(dir string) {
		for _, seen := range dirs {
			if seen == dir {
				return
			}
		}
		dirs = append(dirs, dir)
	}
	if config == "" && goos == "windows" && home != "" {
		config = filepath.Join(home, "AppData", "Roaming")
	}
	var editors []vscodeEditor
	for _, editor := range vscodeEditors {
		if forks || !editor.fork {
			editors = append(editors, editor)
		}
	}
	for _, editor := range editors {
		if editor.product != "" && config != "" {
			add(filepath.Join(config, editor.product, "User"))
		}
	}
	if home == "" {
		return dirs
	}
	if goos != "darwin" && goos != "windows" {
		// a stale XDG_CONFIG_HOME must not hide the default location
		for _, editor := range editors {
			if editor.product != "" {
				add(filepath.Join(home, ".config", editor.product, "User"))
			}
		}
		for _, app := range flatpakVSCode {
			add(filepath.Join(home, ".var", "app", app[0], "config", app[1], "User"))
		}
	}
	for _, editor := range editors {
		if editor.server != "" {
			add(filepath.Join(home, editor.server, "data", "User"))
		}
	}
	return dirs
}

// vscodeGlobalStorageRoots lists an extension's global storage in every
// VS Code style editor and remote server.
func vscodeGlobalStorageRoots(extensionID string) []string {
	var roots []string
	for _, dir := range vscodeUserDirs(true) {
		roots = append(roots, filepath.Join(dir, "globalStorage", extensionID))
	}
	return roots
}
