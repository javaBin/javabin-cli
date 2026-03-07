package config

import (
	"os"
	"path/filepath"
)

const dirName = ".javabin"

// Dir returns the path to ~/.javabin/.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, dirName), nil
}

// EnsureConfigDir creates ~/.javabin/ if it doesn't exist.
func EnsureConfigDir() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0700)
}
