// Command dagda reports local token usage and cost across AI coding
// agents. It reads only files already on disk; its one network call is a
// throttled check for a newer published pricing dataset, skipped under
// DAGDA_OFFLINE=1.
package main

import (
	"os"

	"github.com/semyonfox/dagda/engine/internal/cli"
)

func main() { os.Exit(cli.Main(os.Args)) }
