//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Post-installation welcome dialog (Win32 MessageBoxW).
//
// What this replaces and why. Earlier versions painted a custom window —own
// window class, own message pump, an illustration blitted with StretchDIBits—
// and it failed silently on the tills: with the agent built as -H=windowsgui
// there is no console to report to, so a window that never came up left the
// installer finishing with no visible sign at all, which is the exact problem
// the welcome screen exists to solve. Any GUI toolkit that would have made the
// custom window cheaper (fyne, walk) pulls in CGO and a heavy runtime, and a
// crash inside one of those is just as silent.
//
// How it works now. The dialog is a single call into user32.dll, the same
// mechanism printer_windows.go already uses for ShellExecuteW:
//
//	syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
//
// MessageBoxW is part of the OS: it is always present, it renders its own
// window and runs its own modal message loop, and it needs no window class, no
// device context and no CGO. There is nothing left in this path that can fail
// quietly — either the box is on screen or the call returns 0 and the reason is
// written to the log.
//
// The trade-off is that a MessageBox cannot show the cat illustration. That is
// what "zero dependencies, no silent crashes" costs here, and the tuxedo cat is
// still the icon the operator is told to look for in the tray.

// user32 is resolved lazily: the DLL is only loaded the first time a procedure
// of this file is actually called, which on a normal boot is never.
var (
	user32          = syscall.NewLazyDLL("user32.dll")
	procMessageBoxW = user32.NewProc("MessageBoxW")
)

// MessageBoxW flags. MB_OK gives the single "Aceptar" button and
// MB_ICONINFORMATION the blue information icon: this dialog reports success, it
// does not ask anything. MB_SETFOREGROUND pushes it in front of the installer
// window that is closing right behind it, and MB_TOPMOST keeps it from being
// buried by the POS front-end, which on a till is usually running full screen.
const (
	mbOK              = 0x00000000
	mbIconInformation = 0x00000040
	mbSetForeground   = 0x00010000
	mbTopMost         = 0x00040000
)

const (
	welcomeBoxTitle = "Instalación Exitosa - Cronos POS"
	welcomeBoxText  = "El Agente de Impresión Cronos POS se ha instalado correctamente.\n\n" +
		"El sistema se está ejecutando silenciosamente en segundo plano. " +
		"Puedes gestionarlo desde el icono del gatito en la barra de tareas (junto al reloj)."
)

// showWelcomeWindow opens the native welcome dialog and blocks until the
// operator dismisses it, so it is called from its own goroutine.
//
// Both strings are converted to UTF-16 —the "W" in MessageBoxW— before the
// call, which is what lets the accents and the "¡" of the message reach the
// screen intact. The owner window handle is 0: the agent has no main window,
// and a null owner makes the box a standalone top-level dialog.
func showWelcomeWindow() error {
	title, err := syscall.UTF16PtrFromString(welcomeBoxTitle)
	if err != nil {
		return fmt.Errorf("título no convertible a UTF-16: %w", err)
	}
	text, err := syscall.UTF16PtrFromString(welcomeBoxText)
	if err != nil {
		return fmt.Errorf("mensaje no convertible a UTF-16: %w", err)
	}

	// A zero return value is the only failure MessageBoxW reports (out of
	// memory, or an invalid window handle); anything else is the id of the
	// button the operator pressed.
	ret, _, callErr := procMessageBoxW.Call(
		0, // hWnd: no owner window
		uintptr(unsafe.Pointer(text)),
		uintptr(unsafe.Pointer(title)),
		uintptr(mbOK|mbIconInformation|mbSetForeground|mbTopMost),
	)
	if ret == 0 {
		return fmt.Errorf("MessageBoxW falló: %v", callErr)
	}

	return nil
}
