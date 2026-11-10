//go:build linux

package main

import (
	"errors"
	"io"
)

// isWindowsService always returns false on Linux.
func isWindowsService() bool { return false }

// runAsService always fails on Linux. systemd runs `serve` directly.
func runAsService(string, func(string) string) error {
	return errors.New("the Windows service runs only on Windows")
}

// serviceCommand runs one of install, uninstall, start and stop with systemd.
func serviceCommand(args []string, stdout io.Writer) error {
	s, err := newSystemd(stdout)
	if err != nil {
		return err
	}
	return s.run(args)
}
