package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// RestartExitCode is the exit code that asks the service manager to start the program again.
// It is EX_TEMPFAIL. Any non-zero code works for systemd (Restart=always or on-failure) and for the Windows recovery actions.
const RestartExitCode = 75

// ErrRestart is returned by the program when it ended on purpose and wants to be started again.
// The command maps it to RestartExitCode.
var ErrRestart = errors.New("restart requested")

// RestartMode is how the program gets started again.
type RestartMode int

// The restart modes.
const (
	// RestartByManager means a service manager (Windows service, systemd) starts the program again after an exit with a non-zero code.
	RestartByManager RestartMode = iota
	// RestartByChild means the program started by hand, so it starts the new file as a child itself.
	RestartByChild
)

// ChooseRestartMode picks the restart mode. systemd sets INVOCATION_ID for every unit it starts.
func ChooseRestartMode(windowsService bool, invocationID string) RestartMode {
	if windowsService || invocationID != "" {
		return RestartByManager
	}
	return RestartByChild
}

// CheckRunnable runs "exe version" and fails if it does not end well in a few seconds.
// It tells a broken download from a good one before the old program stops.
func CheckRunnable(exe string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, exe, "version").CombinedOutput(); err != nil {
		return fmt.Errorf("the new program does not run: %w: %s", err, out)
	}
	return nil
}

// StartDetached starts exe with args and env as a new process that does not depend on this one.
// It returns as soon as the process is started.
func StartDetached(exe string, args, env []string) error {
	cmd := exec.Command(exe, args...)
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr // the log of the new program goes where ours went
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
