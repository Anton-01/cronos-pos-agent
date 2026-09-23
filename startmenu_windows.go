//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

// Creating the .lnk through COM (IShellLinkW + IPersistFile).
//
// A Windows shortcut is a binary format, not a text file, and the supported way
// to write one is the shell's own ShellLink object. Two alternatives were
// rejected:
//
//   - Shelling out to PowerShell (New-Object -ComObject WScript.Shell). It is
//     three lines, but the agent runs on locked-down tills where AppLocker or a
//     group policy can block powershell.exe outright, and this is the one step
//     that decides whether the operator can find the program at all.
//   - Writing the .lnk byte layout by hand. It is documented (MS-SHLLINK), but
//     a subtly wrong header produces a shortcut that Explorer silently refuses
//     to open, and nothing in this repository could test it.
//
// COM has neither problem: ole32.dll is part of the operating system, and the
// shell writes a shortcut that is correct by construction.

var (
	ole32                = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
)

const (
	clsctxInprocServer = 0x1
	coinitApartment    = 0x2

	sOK             = 0x00000000
	sFALSE          = 0x00000001
	rpcEChangedMode = 0x80010106
)

func (o *iShellLinkW) Release() {
	syscall.SyscallN(o.vtbl.Release, uintptr(unsafe.Pointer(o)))
}

func (o *iShellLinkW) SetPath(path *uint16) uint32 {
	hr, _, _ := syscall.SyscallN(o.vtbl.SetPath, uintptr(unsafe.Pointer(o)), uintptr(unsafe.Pointer(path)))
	return uint32(hr)
}

func (o *iShellLinkW) SetWorkingDirectory(dir *uint16) uint32 {
	hr, _, _ := syscall.SyscallN(o.vtbl.SetWorkingDirectory, uintptr(unsafe.Pointer(o)), uintptr(unsafe.Pointer(dir)))
	return uint32(hr)
}

func (o *iShellLinkW) SetDescription(description *uint16) uint32 {
	hr, _, _ := syscall.SyscallN(o.vtbl.SetDescription, uintptr(unsafe.Pointer(o)), uintptr(unsafe.Pointer(description)))
	return uint32(hr)
}

// QueryPersistFile asks the link object for its IPersistFile interface, which
// is what actually writes the .lnk to disk.
func (o *iShellLinkW) QueryPersistFile() (*iPersistFile, uint32) {
	var persistFile *iPersistFile
	hr, _, _ := syscall.SyscallN(o.vtbl.QueryInterface,
		uintptr(unsafe.Pointer(o)),
		uintptr(unsafe.Pointer(&iidPersistFile)),
		uintptr(unsafe.Pointer(&persistFile)),
	)
	return persistFile, uint32(hr)
}

func (o *iPersistFile) Release() {
	syscall.SyscallN(o.vtbl.Release, uintptr(unsafe.Pointer(o)))
}

// Save writes the object to fileName. remember=1 makes that path the object's
// current file, which is what the shell expects of a freshly created shortcut.
func (o *iPersistFile) Save(fileName *uint16, remember uintptr) uint32 {
	hr, _, _ := syscall.SyscallN(o.vtbl.Save, uintptr(unsafe.Pointer(o)), uintptr(unsafe.Pointer(fileName)), remember)
	return uint32(hr)
}

// EnsureStartMenuShortcut makes sure the agent has an entry in the Start menu,
// creating it when it is missing.
//
// It runs on every start, like EnsureAutostartRegistered, and for the same
// reason: the shortcut can be missing because the .exe was launched directly
// and no installer ever ran, because an older version never created one, or
// because a disk clean-up removed it. Re-checking costs one stat call and
// guarantees the operator can always find the program by typing its name.
//
// It never creates a second shortcut: if one already exists anywhere the
// installer or an earlier version could have put it — the common Start menu or
// the user's, at the top level or inside a program group — that one is left
// alone and nothing is written.
func EnsureStartMenuShortcut() {
	exe, err := currentExePath()
	if err != nil {
		log.Printf("[start-menu] No se pudo determinar la ruta del ejecutable: %v", err)
		return
	}

	if existing, ok := findStartMenuShortcut(); ok {
		log.Printf("[start-menu] Acceso directo ya presente en '%s'", existing)
		return
	}

	dirs := startMenuProgramsDirs()
	if len(dirs) == 0 {
		log.Println("[start-menu] No se pudo localizar la carpeta Programas del Menú de Inicio")
		return
	}

	// The per-user Start menu comes first and is what gets written: it is
	// always writable without elevation, and it is indexed for the operator who
	// is actually sitting at the till.
	target := filepath.Join(dirs[0], startMenuShortcutName+".lnk")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		log.Printf("[start-menu] No se pudo crear '%s': %v", filepath.Dir(target), err)
		return
	}

	if err := createShellShortcut(target, exe, startMenuShortcutDescription); err != nil {
		log.Printf("[start-menu] No se pudo crear el acceso directo: %v", err)
		return
	}

	log.Printf("[start-menu] Acceso directo creado en '%s' → '%s'", target, exe)
}

// startMenuProgramsDirs lists the "Programs" folders of the Start menu, the
// user's first and the machine-wide one second. Both are indexed by the Start
// menu search; only the first is writable without elevation.
func startMenuProgramsDirs() []string {
	var dirs []string
	for _, env := range []string{"APPDATA", "ProgramData"} {
		if base := os.Getenv(env); base != "" {
			dirs = append(dirs, filepath.Join(base, "Microsoft", "Windows", "Start Menu", "Programs"))
		}
	}
	return dirs
}

// findStartMenuShortcut looks for a shortcut this agent — or an installer —
// already created. Two layouts are checked per folder: the flat one written
// here and by the current [Icons] section, and the program-group layout that
// earlier versions of the installer used.
func findStartMenuShortcut() (string, bool) {
	for _, dir := range startMenuProgramsDirs() {
		candidates := []string{
			filepath.Join(dir, startMenuShortcutName+".lnk"),
			filepath.Join(dir, startMenuShortcutName, startMenuShortcutName+".lnk"),
		}
		for _, candidate := range candidates {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, true
			}
		}
	}
	return "", false
}

// startMenuShortcutStatusLabel reports, for the self-test ticket, whether the
// operator will be able to find the agent from the Start menu. It is printed on
// paper because "no aparece en ningún lado" is otherwise impossible to tell
// apart from "no está instalado".
func startMenuShortcutStatusLabel() string {
	if path, ok := findStartMenuShortcut(); ok {
		return "Sí — " + path
	}
	return "No encontrado"
}

// autostartStatusLabel reports whether the agent will come back on its own
// after a reboot. Together with the shortcut above, it turns the two questions
// a till raises after a restart into something a photo of the ticket answers.
func autostartStatusLabel() string {
	if !isAutostartEnabled() {
		return "No registrado"
	}
	user := os.Getenv("USERNAME")
	if user == "" {
		return "Sí (HKCU\\...\\Run)"
	}
	return "Sí — usuario " + user
}

// createShellShortcut writes the .lnk through the shell's ShellLink object.
//
// The whole sequence is pinned to one OS thread: COM apartments are a property
// of the thread, so a goroutine that migrated between threads mid-sequence
// would be calling into an apartment it never entered.
func createShellShortcut(lnkPath, targetExe, description string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procCoInitializeEx.Call(0, coinitApartment)
	switch uint32(hr) {
	case sOK, sFALSE:
		// S_FALSE means this thread had already entered the same apartment;
		// either way this call owns a reference and has to balance it.
		defer procCoUninitialize.Call()
	case rpcEChangedMode:
		// The thread is already in a different apartment model. The object can
		// still be used, and CoUninitialize is deliberately NOT deferred: this
		// call initialised nothing and must not drop somebody else's reference.
	default:
		return fmt.Errorf("CoInitializeEx falló: 0x%08X", uint32(hr))
	}

	var shellLink *iShellLinkW
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidShellLink)),
		0,
		clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidShellLinkW)),
		uintptr(unsafe.Pointer(&shellLink)),
	)
	if uint32(hr) != sOK {
		return fmt.Errorf("no se pudo crear el objeto ShellLink: 0x%08X", uint32(hr))
	}
	defer shellLink.Release()

	pathPtr, err := syscall.UTF16PtrFromString(targetExe)
	if err != nil {
		return fmt.Errorf("ruta no convertible a UTF-16: %w", err)
	}
	if hr := shellLink.SetPath(pathPtr); hr != sOK {
		return fmt.Errorf("SetPath falló: 0x%08X", hr)
	}

	// The working directory is the binary's own folder. Without it the shortcut
	// inherits whatever directory Explorer happened to be in, which on a till
	// can be a removable drive that is no longer there.
	if dirPtr, err := syscall.UTF16PtrFromString(filepath.Dir(targetExe)); err == nil {
		shellLink.SetWorkingDirectory(dirPtr)
	}

	// The description is the tooltip. A failure here is not worth aborting a
	// shortcut that is otherwise correct.
	if descPtr, err := syscall.UTF16PtrFromString(description); err == nil {
		shellLink.SetDescription(descPtr)
	}

	// No icon is set on purpose: a shortcut to an .exe uses that executable's
	// own icon, and the agent carries the tuxedo cat as a Win32 resource.

	persistFile, queryHR := shellLink.QueryPersistFile()
	if queryHR != sOK {
		return fmt.Errorf("QueryInterface(IPersistFile) falló: 0x%08X", queryHR)
	}
	defer persistFile.Release()

	lnkPtr, err := syscall.UTF16PtrFromString(lnkPath)
	if err != nil {
		return fmt.Errorf("ruta del acceso directo no convertible a UTF-16: %w", err)
	}
	if hr := persistFile.Save(lnkPtr, 1); hr != sOK {
		return fmt.Errorf("IPersistFile::Save falló: 0x%08X", hr)
	}

	return nil
}
