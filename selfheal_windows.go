//go:build windows

package main

import (
	"os/exec"
	"path/filepath"
	"syscall"
)

// startDetached launches another copy of the agent as a fully independent
// Windows process and returns without waiting for it.
//
// How it works: exec.Command only builds the argv, and cmd.Start() is what ends
// up calling CreateProcessW. The two creation flags in SysProcAttr are what make
// the clone usable here:
//
//   - CREATE_NO_WINDOW (createNoWindow) keeps Windows from attaching a console
//     to the child, so no black window flashes on the till's screen.
//   - DETACHED_PROCESS (detachedProcess) gives the child its own console group
//     instead of inheriting the parent's. Without it the child would be tied to
//     a parent that is about to call os.Exit(0), and could be torn down with it.
//
// Start() —as opposed to Run()— returns the moment the process exists, which is
// what lets the caller exit immediately afterwards. cmd.Dir is set to the
// binary's own folder so the child never inherits a working directory that may
// be about to disappear (a removable drive, a temp folder).
func startDetached(exePath string, args ...string) error {
	cmd := exec.Command(exePath, args...)
	cmd.Dir = filepath.Dir(exePath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow | detachedProcess,
	}
	return cmd.Start()
}
