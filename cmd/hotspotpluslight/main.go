// Command hotspotpluslight: `serve | admin set-password | gen-key | service | version`.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
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
	args, envFile, err := extractEnvFile(args)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 2
	}
	var getenv func(string) string
	if len(args) > 0 && (args[0] == "serve" || args[0] == "admin") {
		if getenv, err = envGetter(envFile, os.Getenv); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
	}
	switch {
	case len(args) == 1 && args[0] == "serve":
		if isWindowsService() {
			err = runAsService(envFile, getenv)
			break
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		err = app.Run(ctx, app.Options{Stdout: stdout, Getenv: getenv})
	case len(args) == 1 && args[0] == "version":
		fmt.Fprintln(stdout, version)
	case len(args) == 1 && args[0] == "gen-key":
		err = genKey(stdout)
	case len(args) >= 1 && args[0] == "service":
		err = serviceCommand(args[1:], stdout)
	case len(args) >= 2 && args[0] == "admin" && args[1] == "set-password":
		err = setPassword(args[2:], stdin, stderr, getenv)
	default:
		fmt.Fprintln(stderr, "usage: hotspotpluslight [--env-file <path>] serve | admin set-password | gen-key | service install|uninstall|start|stop | version")
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// genKey prints a new HSP_SECRET_KEY: base64 of 32 random bytes.
func genKey(w io.Writer) error {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, base64.StdEncoding.EncodeToString(b))
	return err
}

// setPassword reads the new admin password and applies it. With --stdin (or when
// stdin is not a terminal) it reads one line; on a terminal it prompts twice
// without echo. The password is never printed.
func setPassword(args []string, stdin io.Reader, stderr io.Writer, getenv func(string) string) error {
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
	if err := app.SetAdminPassword(context.Background(), app.Options{Getenv: getenv}, pw); err != nil {
		return err
	}
	fmt.Fprintln(stderr, "Admin password updated; all admin sessions were ended.")
	return nil
}
