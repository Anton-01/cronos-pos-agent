package main

import "fmt"

// Start Menu shortcut.
//
// The gap this closes. Until v1.9.1 nothing in the agent ever created a
// shortcut: the only thing that did was the [Icons] section of the Inno Setup
// script, and that script did not compile, so no machine ever got one. What the
// agent does do on its own is relocate its binary into a permanent path
// (EnsurePermanentLocation), which leaves a till in the worst of both worlds —
// the program IS installed, in C:\Program Files\CronosAgent, and yet Windows
// cannot find it: the Start menu search indexes the .lnk files under
// "Start Menu\Programs", never a bare .exe sitting in Program Files. Typing
// "Cronos" returned nothing and the operator had to go hunting for the
// executable to start it by hand.
//
// So the shortcut is created by the agent itself, the same way it repairs its
// own autostart entry on every start. It then works on the two paths a till can
// take — installed with the installer, or the .exe run straight from Descargas
// — instead of only the first.
//
// Naming. The shortcut is the product name, which is also what makes it
// findable: the Start menu matches every word of the query against the words of
// the name, so "Cronos", "agent", "POS" and "Cronos agent" all reach
// "Cronos POS Agent".

// startMenuShortcutName is the file name of the shortcut, without the .lnk
// extension. It is also what the operator types to find it, and it must match
// the [Icons] entry of installer/setup.iss so that an install with the
// installer and one without produce the same single entry instead of two.
const startMenuShortcutName = "Cronos POS Agent"

// startMenuShortcutDescription is the tooltip Windows shows for the shortcut.
const startMenuShortcutDescription = "Agente de impresión de Cronos POS: imprime los tickets del punto de venta en las impresoras de este equipo"

// COM types used to create the Windows shortcut.
//
// They live in this file, without a build tag, and not next to the code that
// uses them: the field order of a vtable IS the interface definition, getting
// it wrong calls a different method than the one intended, and there is no
// Windows machine in this project's test environment. Declared here, the layout
// can be checked by TestShellLinkVtableLayout on any platform. They are inert
// everywhere else — plain structs of uintptr that nothing outside
// startmenu_windows.go ever instantiates.

// comGUID is the layout of a Windows GUID as the COM entry points expect it.
type comGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	// CLSID_ShellLink {00021401-0000-0000-C000-000000000046}
	clsidShellLink = comGUID{0x00021401, 0x0000, 0x0000, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	// IID_IShellLinkW {000214F9-0000-0000-C000-000000000046}
	iidShellLinkW = comGUID{0x000214F9, 0x0000, 0x0000, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	// IID_IPersistFile {0000010B-0000-0000-C000-000000000046}
	iidPersistFile = comGUID{0x0000010B, 0x0000, 0x0000, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
)

// COM interfaces, as vtables of function pointers.
//
// A COM object is a pointer to a pointer to a table of methods, and the usual
// way to reach a method in Go is to walk that table with pointer arithmetic.
// That is exactly what "go vet" flags as a misuse of unsafe.Pointer, and it is
// right to: converting a computed uintptr back into a pointer is invisible to
// the garbage collector and unsound in general. Declaring the vtable as an
// ordinary Go struct removes the arithmetic altogether — the compiler computes
// the offsets — and leaves only the one conversion that is documented as valid,
// passing a pointer as a syscall argument.
//
// The field ORDER is the interface definition: it is what maps each name onto
// its slot, so no field may be reordered, renamed away or dropped, and every
// method of the interface has to be listed even when this file never calls it.

type iUnknownVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
}

// iShellLinkWVtbl is IShellLinkW (shobjidl_core.h), in declaration order.
type iShellLinkWVtbl struct {
	iUnknownVtbl
	GetPath             uintptr
	GetIDList           uintptr
	SetIDList           uintptr
	GetDescription      uintptr
	SetDescription      uintptr
	GetWorkingDirectory uintptr
	SetWorkingDirectory uintptr
	GetArguments        uintptr
	SetArguments        uintptr
	GetHotkey           uintptr
	SetHotkey           uintptr
	GetShowCmd          uintptr
	SetShowCmd          uintptr
	GetIconLocation     uintptr
	SetIconLocation     uintptr
	SetRelativePath     uintptr
	Resolve             uintptr
	SetPath             uintptr
}

type iShellLinkW struct {
	vtbl *iShellLinkWVtbl
}

// iPersistFileVtbl is IPersistFile, which extends IPersist — hence GetClassID
// sitting between IUnknown and the file methods.
type iPersistFileVtbl struct {
	iUnknownVtbl
	GetClassID    uintptr
	IsDirty       uintptr
	Load          uintptr
	Save          uintptr
	SaveCompleted uintptr
	GetCurFile    uintptr
}

type iPersistFile struct {
	vtbl *iPersistFileVtbl
}

// formatGUID renders a comGUID in the canonical registry form, so that the
// constants above can be checked against the values in the COM headers.
func formatGUID(g comGUID) string {
	return fmt.Sprintf("%08X-%04X-%04X-%04X-%012X",
		g.Data1, g.Data2, g.Data3, g.Data4[:2], g.Data4[2:])
}
