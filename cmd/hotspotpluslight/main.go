// Command hotspotpluslight: `serve | admin set-password | version`.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"hotspotpluslight/internal/app"
)

// version is overridden at build time: -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	var err error
	switch {
	case len(args) == 1 && args[0] == "serve":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		err = app.Run(ctx, app.Options{Stdout: stdout})
	case len(args) == 1 && args[0] == "version":
		fmt.Fprintln(stdout, version)
	case len(args) == 2 && args[0] == "admin" && args[1] == "set-password":
		// Phase 1 owns this (PLAN 11, E1.06).
		err = errors.New("admin set-password: not implemented yet (Phase 1)")
	default:
		fmt.Fprintln(stderr, "usage: hotspotpluslight serve | admin set-password | version")
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}
