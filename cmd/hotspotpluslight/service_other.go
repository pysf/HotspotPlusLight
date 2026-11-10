//go:build !windows && !linux

package main

import (
	"errors"
	"io"
)

// isWindowsService always returns false outside Windows.
func isWindowsService() bool { return false }

// runAsService always fails outside Windows.
func runAsService(string, func(string) string) error {
	return errors.New("the Windows service runs only on Windows")
}

// serviceCommand always fails outside Windows.
func serviceCommand([]string, io.Writer) error {
	return errors.New("the service commands work only on Windows and Linux")
}
