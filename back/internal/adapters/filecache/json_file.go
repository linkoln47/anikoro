package filecache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"test/internal/ports"
)

func loadJSONFile[T any](path string) (T, error) {
	var value T

	b, err := os.ReadFile(path)
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal(b, &value); err != nil {
		return value, err
	}

	return value, nil
}

func saveJSONFile(path string, perm os.FileMode, label string, logger ports.SyncLogger, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeFileWithChangeLog(path, b, perm, label, logger)
}

func writeFileWithChangeLog(path string, newContent []byte, perm os.FileMode, label string, logger ports.SyncLogger) error {
	oldContent, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			logInfo(logger, "filecache", "creating new file", "label", label, "path", path)
			return writeFileAtomically(path, newContent, perm)
		}
		return err
	}

	if string(oldContent) == string(newContent) {
		logInfo(logger, "filecache", "file content unchanged", "label", label, "path", path)
		return nil
	}

	logInfo(logger, "filecache", "overwriting file", "label", label, "path", path)
	return writeFileAtomically(path, newContent, perm)
}

func writeFileAtomically(path string, content []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	pattern := filepath.Base(path) + ".tmp-*"

	tmpFile, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return err
	}

	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := tmpFile.Chmod(perm); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if _, err := tmpFile.Write(content); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	return os.Rename(tmpPath, path)
}

func logInfo(logger ports.SyncLogger, component, msg string, args ...any) {
	if logger != nil {
		logger.Info(component, msg, args...)
	}
}
