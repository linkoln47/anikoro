package app

import (
	"encoding/json"
	"os"
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

func (a *App) saveJSONFile(path string, perm os.FileMode, label string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return a.writeFileWithChangeLog(path, b, perm, label)
}
