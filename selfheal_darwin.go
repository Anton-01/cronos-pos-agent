//go:build darwin

package main

import (
	"os/exec"
	"path/filepath"
	"syscall"
)

// startDetached launches another copy of the agent as an independent process on
// macOS and returns without waiting for it.
//
// How it works: cmd.Start() forks and execs the binary, and Setsid makes the
// child call setsid(2) right after the fork. That puts it in a brand new session
// with no controlling terminal, which is the POSIX equivalent of the
// DETACHED_PROCESS flag used on Windows: the child no longer belongs to the
// parent's process group, so a signal delivered to that group —or the parent
// exiting a second later— cannot take it down with it.
//
// cmd.Dir is set to the binary's own folder so the child does not inherit a
// working directory that may no longer exist by the time it starts.
func startDetached(exePath string, args ...string) error {
	cmd := exec.Command(exePath, args...)
	cmd.Dir = filepath.Dir(exePath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
