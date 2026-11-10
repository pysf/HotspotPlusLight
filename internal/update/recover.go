package update

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"hotspotpluslight/internal/domain"
)

// File names in the data folder.
const (
	PendingFile  = "update-pending.json"
	MarkerFile   = "update-rolled-back.json"
	databaseFile = "hsp.db"
)

// Outcome is what Recover found.
type Outcome int

// The outcomes of Recover.
const (
	OutcomeNone       Outcome = iota // no update is pending
	OutcomeProbation                 // this is the new program; it must become healthy
	OutcomeRolledBack                // the old files were put back; the caller must restart the program
)

// RecoverResult is the answer of Recover. Pending is set for OutcomeProbation.
type RecoverResult struct {
	Outcome Outcome
	Pending domain.PendingUpdate
}

// ErrCannotRollBack means the old program file or the database copy is missing.
var ErrCannotRollBack = errors.New("the old version or the database copy is missing")

// WritePending saves the state file in dir. It writes a temp file first, so a crash never leaves half a file.
func WritePending(dir string, p domain.PendingUpdate) error {
	b, err := p.Marshal()
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, PendingFile), b)
}

// ClearPending deletes the state file. A missing file is fine.
func ClearPending(dir string) error { return removeIfExists(filepath.Join(dir, PendingFile)) }

// ReadRollbackMarker returns the note left by a rollback, and false when there is none.
func ReadRollbackMarker(dir string) (domain.RollbackMarker, bool) {
	b, err := os.ReadFile(filepath.Join(dir, MarkerFile))
	if err != nil {
		return domain.RollbackMarker{}, false
	}
	m, err := domain.ParseRollbackMarker(b)
	return m, err == nil
}

// DismissRollbackMarker deletes the note. A missing file is fine.
func DismissRollbackMarker(dir string) error { return removeIfExists(filepath.Join(dir, MarkerFile)) }

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// writeAtomic writes data to path through a synced temp file and a rename, with mode 0600.
func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	syncDir(filepath.Dir(path))
	return nil
}

// syncDir makes a rename in dir durable. It does nothing on Windows, and a failure is not an error: the rename is done.
func syncDir(dir string) {
	if runtime.GOOS == "windows" {
		return
	}
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
}

// Recover runs at the very start of the program, before the database is opened. It reads the state file the installer left in dir.
// For the new program it counts the start and returns OutcomeProbation. For a leftover file it deletes it.
// When the new version started too often without becoming healthy it rolls back and returns OutcomeRolledBack.
// A rollback that fails is not an error: the state file is deleted, a note is left and the program starts as it is,
// so a broken state never makes a crash loop. Recover returns an error only when the state file cannot be read or written.
func Recover(dir, running string, now time.Time, log *slog.Logger) (RecoverResult, error) {
	path := filepath.Join(dir, PendingFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return RecoverResult{}, nil
	}
	if err != nil {
		return RecoverResult{}, err
	}
	p, err := domain.ParsePendingUpdate(b)
	if err != nil {
		log.Warn("update: the state file is not valid and was removed", "err", err)
		return RecoverResult{}, ClearPending(dir)
	}
	d := domain.DecidePending(p, running, now)
	switch d.Action {
	case domain.PendingDiscard:
		log.Info("update: removed a leftover state file", "running", running, "pending_for", p.ToVersion)
		return RecoverResult{}, ClearPending(dir)
	case domain.PendingRollback:
		if RollbackOrGiveUp(dir, p, d.Reason, now, log) {
			return RecoverResult{Outcome: OutcomeRolledBack, Pending: p}, nil
		}
		return RecoverResult{}, nil
	}
	p.Tries++
	if err := WritePending(dir, p); err != nil {
		return RecoverResult{}, err
	}
	log.Info("update: first start of the new version, waiting for it to be healthy", "version", p.ToVersion, "start", p.Tries)
	return RecoverResult{Outcome: OutcomeProbation, Pending: p}, nil
}

// UndoTry takes back the count of one start. A clean stop during the health window (a deliberate restart)
// is not a failed start. A missing file is fine.
func UndoTry(dir string) error {
	b, err := os.ReadFile(filepath.Join(dir, PendingFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	p, err := domain.ParsePendingUpdate(b)
	if err != nil || p.Tries == 0 {
		return nil
	}
	p.Tries--
	return WritePending(dir, p)
}

// ClearPendingRetry deletes the state file and tries again after delay when that fails, up to attempts times.
// A file that stays would roll back a healthy version later, so a failure is logged at error level and returned.
func ClearPendingRetry(dir string, log *slog.Logger, attempts int, delay time.Duration) error {
	var err error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			time.Sleep(delay)
		}
		if err = ClearPending(dir); err == nil {
			return nil
		}
	}
	log.Error("update: cannot delete the state file; the next start may roll back a healthy version", "err", err)
	return err
}

// RollbackOrGiveUp rolls back and returns true. If the rollback cannot be done (or was tried too often) it logs an error,
// leaves a note that says so, deletes the state file and returns false. The caller then goes on with the program it has.
func RollbackOrGiveUp(dir string, p domain.PendingUpdate, reason string, now time.Time, log *slog.Logger) bool {
	var err error
	if p.Tries >= domain.PendingGiveUp {
		err = errors.New("too many rollback attempts")
	} else {
		p.Tries++ // a rollback that crashes again and again must end
		if err = WritePending(dir, p); err == nil {
			err = Rollback(dir, p, reason, now, log)
		}
	}
	if err == nil {
		return true
	}
	log.Error("update: cannot roll back, going on with this version", "err", err)
	os.Remove(filepath.Join(dir, databaseFile+".restore"))
	m := domain.RollbackMarker{From: p.FromVersion, To: p.ToVersion, Reason: reason + "; " + err.Error(), At: now.Unix(), NotRestored: true}
	if b, merr := m.Marshal(); merr == nil {
		if werr := writeAtomic(filepath.Join(dir, MarkerFile), b); werr != nil {
			log.Error("update: cannot write the note", "err", werr)
		}
	}
	if cerr := ClearPending(dir); cerr != nil {
		log.Error("update: cannot delete the state file", "err", cerr)
	}
	return false
}

// Rollback puts the database copy and the old program file back and leaves a note for the admin.
// The database comes first and the program file last, because renaming the program file is the commit point:
// a run that stopped before it can start again from the state file. p.DBRestored is saved after the database step,
// and an old program file that is in place already is skipped, so a re-run is safe.
// The bad program file is kept as <exe>.bad and the bad database as hsp.db.failed-<version>.
// The caller must have closed the database. Windows allows renaming a running program file but not overwriting it,
// so every step is a rename.
func Rollback(dir string, p domain.PendingUpdate, reason string, now time.Time, log *slog.Logger) error {
	exeDone := !fileExists(p.OldExe) && fileExists(p.Exe+".bad")
	if !p.DBRestored && !fileExists(p.BackupDB) {
		return fmt.Errorf("%w: %s", ErrCannotRollBack, p.BackupDB)
	}
	if !exeDone && !fileExists(p.OldExe) {
		return fmt.Errorf("%w: %s", ErrCannotRollBack, p.OldExe)
	}
	if !p.DBRestored {
		if err := restoreDatabase(dir, p); err != nil {
			return err
		}
		p.DBRestored = true
		if err := WritePending(dir, p); err != nil {
			return err
		}
	}
	if !exeDone {
		if err := restoreExe(p); err != nil {
			return err
		}
	}
	m := domain.RollbackMarker{From: p.FromVersion, To: p.ToVersion, Reason: reason, At: now.Unix()}
	b, err := m.Marshal()
	if err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(dir, MarkerFile), b); err != nil {
		return err
	}
	if err := ClearPending(dir); err != nil {
		return err
	}
	log.Warn("update: the new version failed and was rolled back; data changed since the backup is lost",
		"from", p.FromVersion, "failed", p.ToVersion, "reason", reason)
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// restoreDatabase copies the backup next to hsp.db, moves the bad database to hsp.db.failed-<version>,
// removes its WAL files and renames the copy into place. It leaves hsp.db as it was if a step fails.
func restoreDatabase(dir string, p domain.PendingUpdate) error {
	db := filepath.Join(dir, databaseFile)
	restored := db + ".restore"
	failed := db + ".failed-" + p.ToVersion
	if err := copyFile(p.BackupDB, restored); err != nil {
		os.Remove(restored)
		return fmt.Errorf("copy the database backup: %w", err)
	}
	if err := removeIfExists(failed); err != nil {
		os.Remove(restored)
		return err
	}
	if err := os.Rename(db, failed); err != nil && !errors.Is(err, os.ErrNotExist) {
		os.Remove(restored)
		return err
	}
	for _, ext := range []string{"-wal", "-shm"} {
		if err := removeIfExists(db + ext); err != nil {
			os.Rename(failed, db)
			os.Remove(restored)
			return err
		}
	}
	if err := os.Rename(restored, db); err != nil {
		os.Rename(failed, db)
		os.Remove(restored)
		return err
	}
	return nil
}

// restoreExe renames the new program file to <exe>.bad and the old one back. It undoes the first rename if the second fails.
func restoreExe(p domain.PendingUpdate) error {
	bad := p.Exe + ".bad"
	if err := removeIfExists(bad); err != nil {
		return err
	}
	if err := os.Rename(p.Exe, bad); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(p.OldExe, p.Exe); err != nil {
		os.Rename(bad, p.Exe)
		return err
	}
	return nil
}

// copyFile copies src to dst with mode 0600 and syncs it to disk.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}
