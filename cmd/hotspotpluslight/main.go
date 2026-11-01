// Command hotspotpluslight: `serve | admin set-password | version`.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"

	"hotspotpluslight/internal/app"
)

// version is overridden at build time: -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	var err error
	switch {
	case len(args) == 1 && args[0] == "serve":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		err = app.Run(ctx, app.Options{Stdout: stdout})
	case len(args) == 1 && args[0] == "version":
		fmt.Fprintln(stdout, version)
	case len(args) >= 2 && args[0] == "admin" && args[1] == "set-password":
		err = setPassword(args[2:], stdin, stderr)
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

// setPassword reads the new admin password and applies it. With --stdin (or when
// stdin is not a terminal) it reads one line; on a terminal it prompts twice
// without echo. The password is never printed.
func setPassword(args []string, stdin io.Reader, stderr io.Writer) error {
	fromStdin := false
	for _, a := range args {
		if a != "--stdin" {
			return fmt.Errorf("unknown argument %q (usage: admin set-password [--stdin])", a)
		}
		fromStdin = true
	}
	var pw string
	if f, ok := stdin.(*os.File); ok && !fromStdin && term.IsTerminal(int(f.Fd())) {
		fd := int(f.Fd())
		fmt.Fprint(stderr, "New admin password: ")
		a, err := term.ReadPassword(fd)
		fmt.Fprintln(stderr)
		if err != nil {
			return err
		}
		fmt.Fprint(stderr, "Repeat password: ")
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(stderr)
		if err != nil {
			return err
		}
		if string(a) != string(b) {
			return errors.New("passwords do not match")
		}
		pw = string(a)
	} else {
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && line == "" {
			return errors.New("no password on stdin")
		}
		pw = strings.TrimRight(line, "\r\n")
	}
	if err := app.SetAdminPassword(context.Background(), app.Options{}, pw); err != nil {
		return err
	}
	fmt.Fprintln(stderr, "Admin password updated; all admin sessions were ended.")
	return nil
}
