//go:build windows

package platform

import (
	"os/exec"
	"syscall"
	"unsafe"
)

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	user32          = syscall.NewLazyDLL("user32.dll")
	procCreateMutex = kernel32.NewProc("CreateMutexW")
	procCloseHandle = kernel32.NewProc("CloseHandle")
	procMessageBox  = user32.NewProc("MessageBoxW")
)

const errorAlreadyExists = syscall.Errno(183)

// AcquireMutex returns an already-running flag. The caller must call release
// when the process exits.
func AcquireMutex(name string) (release func(), already bool, err error) {
	ptr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, false, err
	}
	handle, _, callErr := procCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(ptr)))
	if handle == 0 {
		return nil, false, callErr
	}
	if callErr == errorAlreadyExists {
		procCloseHandle.Call(handle)
		return nil, true, nil
	}
	return func() { procCloseHandle.Call(handle) }, false, nil
}

func OpenBrowser(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

func MessageBox(title, text string) {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	textPtr, _ := syscall.UTF16PtrFromString(text)
	procMessageBox.Call(0, uintptr(unsafe.Pointer(textPtr)), uintptr(unsafe.Pointer(titlePtr)), 0x10)
}
