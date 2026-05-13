package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Snapshot struct {
	OriginalPath string
	BackupPath   string
	Timestamp    time.Time
}

var snapshots []Snapshot

func Backup(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Nothing to backup
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

	snapshots = append(snapshots, Snapshot{
		OriginalPath: path,
		BackupPath:   backupPath,
		Timestamp:    time.Now(),
	})
	return nil
}

func Undo() (string, error) {
	if len(snapshots) == 0 {
		return "", fmt.Errorf("no snapshots available to undo")
	}

	last := snapshots[len(snapshots)-1]
	snapshots = snapshots[:len(snapshots)-1]

	data, err := os.ReadFile(last.BackupPath)
	if err != nil {
		return "", fmt.Errorf("failed to read backup file %s: %w", last.BackupPath, err)
	}

	if err := os.WriteFile(last.OriginalPath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to restore file %s: %w", last.OriginalPath, err)
	}

	// Clean up backup file
	os.Remove(last.BackupPath)

	return last.OriginalPath, nil
}
