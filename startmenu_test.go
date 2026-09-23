package main

import (
	"testing"
	"unsafe"
)

// The field order of a vtable is the interface definition: each name maps onto
// a slot, and a field inserted, dropped or reordered silently calls a different
// method than the one intended — SetPath would become Resolve, and the shortcut
// would come out corrupt or take the process down.
//
// Nothing else in this project can catch that: the code only compiles into a
// Windows binary and there is no Windows machine in the test environment. So
// the slot numbers from the COM headers are asserted here, on any platform,
// against the offsets the Go compiler computes.
func TestShellLinkVtableLayout(t *testing.T) {
	const slotSize = unsafe.Sizeof(uintptr(0))

	// IShellLinkW, counted from IUnknown: QueryInterface 0, AddRef 1,
	// Release 2, and the interface's own methods from 3 onwards.
	shellLink := map[string]struct {
		offset uintptr
		slot   uintptr
	}{
		"QueryInterface":      {unsafe.Offsetof(iShellLinkWVtbl{}.QueryInterface), 0},
		"Release":             {unsafe.Offsetof(iShellLinkWVtbl{}.Release), 2},
		"SetDescription":      {unsafe.Offsetof(iShellLinkWVtbl{}.SetDescription), 7},
		"SetWorkingDirectory": {unsafe.Offsetof(iShellLinkWVtbl{}.SetWorkingDirectory), 9},
		"SetPath":             {unsafe.Offsetof(iShellLinkWVtbl{}.SetPath), 20},
	}
	for name, want := range shellLink {
		if got := want.offset / slotSize; got != want.slot {
			t.Errorf("IShellLinkW::%s está en la ranura %d, la cabecera COM dice %d", name, got, want.slot)
		}
	}

	// IPersistFile extends IPersist, so GetClassID takes slot 3 and the file
	// methods start at 4.
	persistFile := map[string]struct {
		offset uintptr
		slot   uintptr
	}{
		"Release":    {unsafe.Offsetof(iPersistFileVtbl{}.Release), 2},
		"GetClassID": {unsafe.Offsetof(iPersistFileVtbl{}.GetClassID), 3},
		"Save":       {unsafe.Offsetof(iPersistFileVtbl{}.Save), 6},
	}
	for name, want := range persistFile {
		if got := want.offset / slotSize; got != want.slot {
			t.Errorf("IPersistFile::%s está en la ranura %d, la cabecera COM dice %d", name, got, want.slot)
		}
	}

	// Y el objeto COM es un puntero a la vtable y nada más: un campo de más en
	// la envoltura desplazaría la tabla entera.
	if size := unsafe.Sizeof(iShellLinkW{}); size != slotSize {
		t.Errorf("iShellLinkW ocupa %d bytes, debe ser sólo el puntero a la vtable (%d)", size, slotSize)
	}
	if size := unsafe.Sizeof(iPersistFile{}); size != slotSize {
		t.Errorf("iPersistFile ocupa %d bytes, debe ser sólo el puntero a la vtable (%d)", size, slotSize)
	}
}

// The GUIDs are the identity of the COM classes being asked for: a wrong digit
// means CoCreateInstance returns "class not registered" and no shortcut is ever
// created, on every machine, silently.
func TestShellLinkGUIDs(t *testing.T) {
	cases := map[string]struct {
		guid comGUID
		want string
	}{
		"CLSID_ShellLink":  {clsidShellLink, "00021401-0000-0000-C000-000000000046"},
		"IID_IShellLinkW":  {iidShellLinkW, "000214F9-0000-0000-C000-000000000046"},
		"IID_IPersistFile": {iidPersistFile, "0000010B-0000-0000-C000-000000000046"},
	}

	for name, tc := range cases {
		if got := formatGUID(tc.guid); got != tc.want {
			t.Errorf("%s = %s, se esperaba %s", name, got, tc.want)
		}
	}
}
