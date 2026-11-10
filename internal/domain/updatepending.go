package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Limits of the first start after an update. The new program is rolled back when it started this many times
// without becoming healthy. A start that ends on purpose (a clean stop) does not count.
// There is no age limit: a healthy new version must never be rolled back later.
// PendingGiveUp is the number of tries after which a rollback is not tried again.
const (
	PendingMaxTries = 2
	PendingGiveUp   = PendingMaxTries + 3
)

// PendingUpdate is the state file the installer writes right before it swaps the program file.
// The new program deletes the file when it is healthy. If the file stays, the update is rolled back.
type PendingUpdate struct {
	FromVersion string `json:"from_version"`
	ToVersion   string `json:"to_version"`
	StartedAt   int64  `json:"started_at"`            // Unix seconds UTC
	Exe         string `json:"exe"`                   // the program file, now the new version
	OldExe      string `json:"old_exe"`               // the old version, to put back
	BackupDB    string `json:"backup_db"`             // the database copy made before the swap
	Tries       int    `json:"tries"`                 // how many starts did not end on purpose; also counts rollback attempts
	DBRestored  bool   `json:"db_restored,omitempty"` // a rollback restored the database already; a re-run skips that step
}

// ParsePendingUpdate reads and checks the state file. It returns an error for text that is not valid.
func ParsePendingUpdate(b []byte) (PendingUpdate, error) {
	var p PendingUpdate
	if err := json.Unmarshal(b, &p); err != nil {
		return PendingUpdate{}, err
	}
	switch {
	case strings.TrimSpace(p.ToVersion) == "":
		return PendingUpdate{}, errors.New("to_version is empty")
	case p.Exe == "" || p.OldExe == "" || p.BackupDB == "":
		return PendingUpdate{}, errors.New("exe, old_exe and backup_db must be set")
	case p.StartedAt <= 0:
		return PendingUpdate{}, errors.New("started_at is not valid")
	case p.Tries < 0:
		return PendingUpdate{}, errors.New("tries is negative")
	}
	return p, nil
}

// Marshal returns the state file text.
func (p PendingUpdate) Marshal() ([]byte, error) { return json.MarshalIndent(p, "", "  ") }

// RollbackMarker is the note left after a rollback. The admin sees it until it is dismissed.
type RollbackMarker struct {
	From   string `json:"from"`   // the version that was restored
	To     string `json:"to"`     // the version that failed
	Reason string `json:"reason"` // short text without a final dot, such as "the new version did not start"
	At     int64  `json:"at"`     // Unix seconds UTC
	// NotRestored is true when the rollback itself failed. The new version is still installed.
	NotRestored bool `json:"not_restored,omitempty"`
}

// Message is the text the admin sees. It also says that data written after the backup is gone.
func (m RollbackMarker) Message() string {
	if m.NotRestored {
		return "The last update to " + m.To + " failed, and the old version could not be put back (" + m.Reason + "). " +
			"The new version is still installed. Look at the server log."
	}
	return "The last update to " + m.To + " failed and was rolled back (" + m.Reason + "). " +
		"Data that changed after the backup was made, just before the update, is lost."
}

// ParseRollbackMarker reads the marker file.
func ParseRollbackMarker(b []byte) (RollbackMarker, error) {
	var m RollbackMarker
	if err := json.Unmarshal(b, &m); err != nil {
		return RollbackMarker{}, err
	}
	if m.To == "" || m.At <= 0 {
		return RollbackMarker{}, errors.New("the marker is not valid")
	}
	return m, nil
}

// Marshal returns the marker file text.
func (m RollbackMarker) Marshal() ([]byte, error) { return json.MarshalIndent(m, "", "  ") }

// PendingAction is what a start does about a state file.
type PendingAction int

// The actions of DecidePending.
const (
	PendingProbation PendingAction = iota // this is the new program: count the start and wait for the health check
	PendingDiscard                        // the file is a leftover: delete it
	PendingRollback                       // the new version failed: put the old one back
)

// PendingDecision is the answer of DecidePending. Reason is set for a rollback.
type PendingDecision struct {
	Action PendingAction
	Reason string
}

// sameVersion compares two version texts, ignoring a leading "v". Texts that are not versions must be equal.
func sameVersion(a, b string) bool {
	va, ok1 := ParseVersion(a)
	vb, ok2 := ParseVersion(b)
	if ok1 && ok2 {
		return va == vb
	}
	return a == b
}

// DecidePending says what a program with version running does when it finds state file p at time now.
// A different program than the new one (the old file was put back, or an old file was started) discards the file.
func DecidePending(p PendingUpdate, running string, now time.Time) PendingDecision {
	switch {
	case !sameVersion(running, p.ToVersion):
		return PendingDecision{Action: PendingDiscard}
	case p.Tries >= PendingMaxTries:
		return PendingDecision{PendingRollback, "the new version did not start"}
	}
	return PendingDecision{Action: PendingProbation}
}
