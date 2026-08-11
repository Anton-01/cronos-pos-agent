package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/getlantern/systray"
)

// flushRestartFlagName is the hidden command-line flag that marks an instance
// as "spawned by the Reiniciar y Limpiar (Debug) menu item". It is not meant
// for operators: the agent passes it to itself when it clones its own process,
// and it is the only signal the new instance has that it must run the cleanup
// routine before binding the port.
const flushRestartFlagName = "flush-restart"

// portReleaseGrace is how long the restarted instance waits before it touches
// the network. The old process is still alive when the new one starts —it exits
// milliseconds later— and the listening socket is torn down by the operating
// system asynchronously, after the process is gone. 1.5 s covers that teardown
// with room to spare while still being unnoticeable to whoever clicked the menu.
const portReleaseGrace = 1500 * time.Millisecond

// tempPrintArtifacts lists the glob patterns of the throwaway files the printing
// paths write into the system temp folder. Each of them is deleted by the code
// that creates it, so a leftover means the agent died mid-print (a crash, a kill
// from killOrphanInstances, a power cut on the till). The flush restart is
// exactly the moment to sweep them.
var tempPrintArtifacts = []string{
	"cronos-pdf-*.pdf",    // printPDF: the PDF handed to ShellExecuteW / lp
	"cronos-ticket-*.bin", // rawPrint on macOS: the ESC/POS payload sent to lp -o raw
	"cronos-app-icon.ico", // welcome window: icon extracted for LoadImageW
}

// RestartWithFlush clones the running agent into a brand new process and ends
// this one, which is what the "Reiniciar y Limpiar (Debug)" menu item does.
//
// How the process is cloned: os.Executable() returns the path of the binary
// that is currently running, so the agent can launch *itself* without knowing
// where it was installed. That path is handed to startDetached() —the
// platform-specific wrapper around exec.Command— together with two flags:
//
//   - --flush-restart tells the child to run RunFlushCleanup() before it opens
//     the port, which is the whole point of the restart.
//   - --relaunched tells it to skip EnsurePermanentLocation(). This process
//     already cleared that gate, so the child would only relocate itself and
//     relaunch a third process for nothing.
//
// cmd.Start() returns as soon as the operating system has created the child: it
// does not wait for it to finish, which is what allows this process to leave
// immediately. The child is detached, so it outlives its parent.
//
// On success this function never returns — it ends the process with exit code 0.
// It only returns an error when the child could not be spawned, in which case
// the caller must keep the current agent running: killing it would leave the
// till with no agent at all.
func RestartWithFlush() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("no se pudo determinar la ruta del ejecutable: %w", err)
	}

	log.Printf("[flush-restart] Lanzando una nueva instancia desde '%s'", exe)

	if err := startDetached(exe, "--"+flushRestartFlagName, "--relaunched"); err != nil {
		return fmt.Errorf("no se pudo lanzar la nueva instancia: %w", err)
	}

	// The successor is already running: this process leaves right away, but not
	// abruptly. systray.Quit() removes the icon from the tray so the operator
	// never sees two cats at once, and onExit() —idempotent thanks to its
	// sync.Once— shuts the HTTP server down *synchronously* here instead of
	// trusting the tray loop to get there before os.Exit() kills everything.
	// Closing the server frees the port before the child stops waiting.
	log.Println("[flush-restart] Nueva instancia lanzada, esta instancia se cierra")
	systray.Quit()
	onExit()

	os.Exit(0)
	return nil // unreachable: os.Exit does not return
}

// RunFlushCleanup is the cleanup routine of the restarted instance. main() runs
// it as soon as the hidden --flush-restart flag is parsed, which is before the
// HTTP server binds its port and before the tray icon turns green: everything
// this function does must be finished by the time the agent claims to be
// operational.
//
// It does two things: it waits for the operating system to release the port the
// previous process was listening on, and it deletes the disposable files that a
// previous run may have left behind.
func RunFlushCleanup() {
	log.Printf("[flush-restart] Limpieza iniciada — esperando %s a que el sistema libere el puerto", portReleaseGrace)

	// Why a fixed wait and not an immediate bind: the process that spawned this
	// one was still alive when this one started, and its listening socket is
	// only reclaimed by the kernel once it is fully gone. Binding too early
	// fails, and a failed bind is not harmless here: ResolvePort() would fall
	// back to 9101 and the frontend, which points at 9100, would stop finding
	// the agent after a restart that was supposed to fix things.
	time.Sleep(portReleaseGrace)

	removed := purgeTempPrintArtifacts() + purgeRotatedLogs()

	// log's output is an io.MultiWriter over stdout and the rotating log file
	// (see SetupLogger), so this line reaches standard output as required and is
	// also kept on disk for support.
	log.Printf("[flush-restart] Limpieza completada correctamente — %d archivo(s) desechable(s) eliminado(s), puerto listo", removed)
}

// purgeTempPrintArtifacts deletes the leftover print files of previous runs from
// the system temp folder and returns how many were removed. Files still held
// open by another process simply fail to delete: the error is logged and the
// cleanup carries on, because a stale temp file must never block a restart.
func purgeTempPrintArtifacts() int {
	tempDir := os.TempDir()
	removed := 0

	for _, pattern := range tempPrintArtifacts {
		matches, err := filepath.Glob(filepath.Join(tempDir, pattern))
		if err != nil {
			// Only a malformed pattern gets here, never a missing file.
			log.Printf("[flush-restart] Patrón temporal inválido '%s': %v", pattern, err)
			continue
		}

		for _, path := range matches {
			if err := os.Remove(path); err != nil {
				log.Printf("[flush-restart] No se pudo borrar '%s': %v", path, err)
				continue
			}
			log.Printf("[flush-restart] Temporal de impresión eliminado: %s", path)
			removed++
		}
	}

	return removed
}

// purgeRotatedLogs deletes the rotated copies of the agent log (cronos-agent.log.1
// to .3) from the data directory and returns how many were removed.
//
// The *active* log file is deliberately left alone: the logger already has it
// open —Windows would refuse to delete it anyway— and it is where the record of
// this very cleanup is being written. Only the archived rotations go, which is
// what makes the next support log start from an unpolluted baseline.
func purgeRotatedLogs() int {
	base := filepath.Join(agentDir(), logFileName)
	removed := 0

	for i := 1; i <= logMaxBackups; i++ {
		path := fmt.Sprintf("%s.%d", base, i)
		if err := os.Remove(path); err != nil {
			if !os.IsNotExist(err) {
				log.Printf("[flush-restart] No se pudo borrar '%s': %v", path, err)
			}
			continue
		}
		log.Printf("[flush-restart] Log rotado eliminado: %s", path)
		removed++
	}

	return removed
}
