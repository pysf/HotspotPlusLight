//go:build !windows

package main

import (
	"errors"
	"io"
)

func isWindowsService() bool { return false }

func runAsService(string, func(string) string) error {
	return errors.New("the Windows service runs only on Windows")
}

func serviceCommand([]string, io.Writer) error {
	return errors.New("the service commands work only on Windows")
}
