// Command herdr-scope is a Herdr plugin that turns the sidebar's Agents list
// into a readable panel: who is working, who waits on you, who is done.
package main

import (
	"context"
	"os"

	"github.com/kriuchkov/herdr-scope/internal/bootstrap"
)

// version overrides bootstrap.Version at build time
// (-ldflags "-X main.version=..."), for builds between releases.
var version = ""

func main() {
	v := bootstrap.Version
	if version != "" {
		v = version
	}
	os.Exit(bootstrap.Main(context.Background(), bootstrap.Build{Version: v}, os.Args[1:], os.Stdout, os.Stderr))
}
