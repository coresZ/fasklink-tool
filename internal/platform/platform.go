package platform

import (
	"os"
	"path/filepath"
)

// ExecutableDir returns the directory containing the running exe.
func ExecutableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}
