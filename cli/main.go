package main

import (
	"fmt"
	"os"
)

// Version is set by goreleaser via ldflags.
var Version = "dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: relay <health|ws|core|jq|version> [args...]")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "health":
		runHealth(os.Args[2:])
	case "ws":
		runProxy("ws", os.Args[2:])
	case "core":
		runProxy("core", os.Args[2:])
	case "jq":
		runJQ(os.Args[2:])
	case "version":
		fmt.Println(Version)
	default:
		runProxy(os.Args[1], os.Args[2:])
	}
}
