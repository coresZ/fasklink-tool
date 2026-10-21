//go:build !windows

package platform

import (
	"fmt"
	"os/exec"
	"runtime"
)

func AcquireMutex(string) (func(), bool, error) { return func() {}, false, nil }

// OpenBrowser opens url with the platform opener (open on macOS, xdg-open on Linux).
func OpenBrowser(url string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", url).Start()
	}
	opener, err := exec.LookPath("xdg-open")
	if err != nil {
		return fmt.Errorf("xdg-open not found: %w", err)
	}
	return exec.Command(opener, url).Start()
}

func MessageBox(string, string) {}
