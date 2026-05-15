package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Snapshot struct {
	OriginalPath string
	BackupPath   string
	Timestamp    time.Time
}

var (
	mu        sync.Mutex
	snapshots []Snapshot
	redoStack []Snapshot
)

func Backup(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	dir := filepath.Join(".opencode", "snapshots")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	backupName := fmt.Sprintf("%d_%s", time.Now().UnixNano(), filepath.Base(path))
	backupPath := filepath.Join(dir, backupName)

	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return err
	}

	mu.Lock()
	snapshots = append(snapshots, Snapshot{
		OriginalPath: path,
		BackupPath:   backupPath,
		Timestamp:    time.Now(),
	})
	mu.Unlock()
	return nil
}

func Undo() (string, error) {
	mu.Lock()
	if len(snapshots) == 0 {
		mu.Unlock()
		return "", fmt.Errorf("no snapshots available to undo")
	}
	last := snapshots[len(snapshots)-1]
	snapshots = snapshots[:len(snapshots)-1]
	mu.Unlock()

	currentData, _ := os.ReadFile(last.OriginalPath)
	redoBackupPath := last.BackupPath + ".redo"
	if err := os.WriteFile(redoBackupPath, currentData, 0644); err != nil {
		return "", fmt.Errorf("failed to save redo backup for %s: %w", last.OriginalPath, err)
	}

	mu.Lock()
	redoStack = append(redoStack, Snapshot{
		OriginalPath: last.OriginalPath,
		BackupPath:   redoBackupPath,
		Timestamp:    time.Now(),
	})
	mu.Unlock()

	data, err := os.ReadFile(last.BackupPath)
	if err != nil {
		return "", fmt.Errorf("failed to read backup file %s: %w", last.BackupPath, err)
	}

	if err := os.WriteFile(last.OriginalPath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to restore file %s: %w", last.OriginalPath, err)
	}

	os.Remove(last.BackupPath) //nolint:errcheck
	return last.OriginalPath, nil
}

func Redo() (string, error) {
	mu.Lock()
	if len(redoStack) == 0 {
		mu.Unlock()
		return "", fmt.Errorf("nothing to redo")
	}
	last := redoStack[len(redoStack)-1]
	redoStack = redoStack[:len(redoStack)-1]
	mu.Unlock()

	data, err := os.ReadFile(last.BackupPath)
	if err != nil {
		return "", fmt.Errorf("failed to read redo file %s: %w", last.BackupPath, err)
	}

	if err := Backup(last.OriginalPath); err != nil {
		return "", fmt.Errorf("failed to backup before redo: %w", err)
	}

	if err := os.WriteFile(last.OriginalPath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to restore file %s: %w", last.OriginalPath, err)
	}

	os.Remove(last.BackupPath) //nolint:errcheck
	return last.OriginalPath, nil
}

func DiscardRecent(count int) error {
	if count < 0 {
		return fmt.Errorf("invalid discard count %d", count)
	}
	if count == 0 {
		return nil
	}

	mu.Lock()
	if count > len(snapshots) {
		mu.Unlock()
		return fmt.Errorf("cannot discard %d snapshots; only %d available", count, len(snapshots))
	}
	removed := append([]Snapshot(nil), snapshots[len(snapshots)-count:]...)
	snapshots = snapshots[:len(snapshots)-count]
	mu.Unlock()

	var firstErr error
	for _, s := range removed {
		if err := os.Remove(s.BackupPath); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = fmt.Errorf("failed to remove snapshot %s: %w", s.BackupPath, err)
		}
	}
	return firstErr
}
