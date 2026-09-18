package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// Static checks over installer/setup.iss and the Win32 resource inputs.
//
// Why these live in the Go test suite. The Inno Setup compiler only runs on
// Windows, so nothing in CI ever looked at that script — and it showed: the
// version shipped as 1.8.0 did not compile at all. A rewrite had dropped the
// "#define AppGuid" and four [Code] functions while leaving every call site in
// place, so ISCC would have rejected it on the first line that used them. The
// consequence on a till is not subtle: without AppId there is no uninstall
// registry key, which is exactly the entry Windows reads to list a program
// under "Agregar o quitar programas" and to remove it.
//
// These tests cannot replace ISCC, and they do not try to. They catch the class
// of defect that shipped: a reference with nothing behind it, a version that
// drifted out of step with the agent's, and an encoding that turns the Spanish
// messages into mojibake.

const installerScriptPath = "installer/setup.iss"

func readInstallerScript(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile(installerScriptPath)
	if err != nil {
		t.Fatalf("no se pudo leer %s: %v", installerScriptPath, err)
	}
	return string(data)
}

// Inno Setup 6 reads a .iss as UTF-8 only when it starts with a byte order
// mark; without one it falls back to the system ANSI code page. The script
// carries accented Spanish in every message the operator reads, so a missing
// BOM does not fail the build — it silently prints "actualizacioÌn" on the
// screen of a till.
func TestInstallerScriptIsUTF8WithBOM(t *testing.T) {
	data, err := os.ReadFile(installerScriptPath)
	if err != nil {
		t.Fatalf("no se pudo leer %s: %v", installerScriptPath, err)
	}

	if !strings.HasPrefix(string(data), "\ufeff") {
		t.Error("setup.iss no empieza por el BOM de UTF-8: Inno lo leería como ANSI y los acentos de los mensajes saldrían corruptos")
	}
}

// Every "{#Name}" in the script has to have a "#define Name" behind it. This is
// the exact check that the 1.8.0 script fails.
func TestInstallerPreprocessorDefines(t *testing.T) {
	src := readInstallerScript(t)

	defined := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^#define\s+(\w+)`).FindAllStringSubmatch(src, -1) {
		defined[m[1]] = true
	}

	for _, m := range regexp.MustCompile(`\{#(\w+)\}`).FindAllStringSubmatch(src, -1) {
		if !defined[m[1]] {
			t.Errorf("{#%s} se usa en setup.iss pero no hay ningún '#define %s'", m[1], m[1])
		}
	}
}

// AppId is what produces the uninstall registry key, and therefore the entry in
// "Agregar o quitar programas". It must resolve to a literal GUID: an AppId
// built from an undefined symbol leaves the program installed with no way to
// find it or remove it from Windows.
func TestInstallerAppIdResolvesToGUID(t *testing.T) {
	src := resolveInstallerDefines(readInstallerScript(t))

	appID := regexp.MustCompile(`(?m)^AppId=(.+)$`).FindStringSubmatch(src)
	if appID == nil {
		t.Fatal("setup.iss no declara AppId: sin él no hay entrada de desinstalación en Windows")
	}

	// Inno escapes a literal "{" by doubling it, so the value on disk reads
	// "{{GUID}" and the AppId Windows sees is "{GUID}".
	value := strings.TrimSpace(appID[1])
	if !regexp.MustCompile(`^\{\{?[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}\}$`).MatchString(value) {
		t.Errorf("AppId = %q, se esperaba un GUID literal", value)
	}

	for _, directive := range []string{"Uninstallable=yes", "CreateUninstallRegKey=yes"} {
		if !strings.Contains(src, directive) {
			t.Errorf("falta la directiva %q: es la que deja desinstalar el programa desde Windows", directive)
		}
	}
}

// A "{code:Name}" constant is resolved by Inno at install time by calling
// Name(Param: String): String. A missing function, or one with another
// signature, is a compile error in ISCC and would only be found on a Windows
// build machine.
func TestInstallerCodeConstantsExist(t *testing.T) {
	src := readInstallerScript(t)
	code := installerCodeSection(t, src)

	for _, m := range regexp.MustCompile(`\{code:(\w+)`).FindAllStringSubmatch(src, -1) {
		name := m[1]
		signature := regexp.MustCompile(`(?i)function\s+` + regexp.QuoteMeta(name) + `\s*\(\s*Param\s*:\s*String\s*\)\s*:\s*String`)
		if !signature.MatchString(code) {
			t.Errorf("{code:%s} se usa pero no existe 'function %s(Param: String): String' en [Code]", name, name)
		}
	}
}

// Pascal Script resolves identifiers in one pass: a function called above its
// own declaration does not compile. The check walks every function and
// procedure of the [Code] section and compares the position of its declaration
// with the first place its name is used.
func TestInstallerNoUseBeforeDeclaration(t *testing.T) {
	code := stripPascalNoise(installerCodeSection(t, readInstallerScript(t)))

	declaration := regexp.MustCompile(`(?mi)^\s*(?:function|procedure)\s+(\w+)`)
	for _, loc := range declaration.FindAllStringSubmatchIndex(code, -1) {
		name := code[loc[2]:loc[3]]

		uses := regexp.MustCompile(`(?i)\b`+regexp.QuoteMeta(name)+`\b`).FindAllStringIndex(code, -1)
		for _, use := range uses {
			if use[0] >= loc[2] {
				break // la propia declaración o algo posterior
			}
			t.Errorf("'%s' se usa en la posición %d pero se declara en la %d: Pascal Script exige declarar antes de usar",
				name, use[0], loc[2])
			break
		}
	}
}

// An unbalanced begin/end is the other failure that only ISCC would report.
func TestInstallerBlocksAreBalanced(t *testing.T) {
	code := stripPascalNoise(installerCodeSection(t, readInstallerScript(t)))

	depth := 0
	for _, m := range regexp.MustCompile(`(?i)\b(begin|case|record|try|end)\b`).FindAllString(code, -1) {
		if strings.EqualFold(m, "end") {
			depth--
		} else {
			depth++
		}
		if depth < 0 {
			t.Fatal("hay un 'end' de más en la sección [Code]")
		}
	}
	if depth != 0 {
		t.Errorf("faltan %d 'end' en la sección [Code]", depth)
	}
}

// The version is written in four places that have to agree, and they have
// drifted apart before: the agent reports one number, the installer registers
// another under "Agregar o quitar programas", and the .exe shows a third in its
// properties.
func TestVersionIsConsistentAcrossArtifacts(t *testing.T) {
	src := readInstallerScript(t)

	installerVersion := regexp.MustCompile(`(?m)^#define\s+AppVersion\s+"([^"]+)"`).FindStringSubmatch(src)
	if installerVersion == nil {
		t.Fatal("setup.iss no declara AppVersion")
	}
	if installerVersion[1] != AgentVersion {
		t.Errorf("setup.iss declara la versión %q y el agente es la %q", installerVersion[1], AgentVersion)
	}

	manifest, err := os.ReadFile("app.manifest")
	if err != nil {
		t.Fatalf("no se pudo leer app.manifest: %v", err)
	}
	wantManifest := fmt.Sprintf(`version="%s.0"`, AgentVersion)
	if !strings.Contains(string(manifest), wantManifest) {
		t.Errorf("app.manifest no declara %s: el .exe enlazaría la versión anterior en su assemblyIdentity", wantManifest)
	}

	raw, err := os.ReadFile("versioninfo.json")
	if err != nil {
		t.Fatalf("no se pudo leer versioninfo.json: %v", err)
	}
	var vi struct {
		StringFileInfo struct {
			FileVersion    string
			ProductVersion string
		}
	}
	if err := json.Unmarshal(raw, &vi); err != nil {
		t.Fatalf("versioninfo.json no es JSON válido: %v", err)
	}
	for field, value := range map[string]string{
		"FileVersion":    vi.StringFileInfo.FileVersion,
		"ProductVersion": vi.StringFileInfo.ProductVersion,
	} {
		if value != AgentVersion+".0" {
			t.Errorf("versioninfo.json declara %s=%q, se esperaba %q", field, value, AgentVersion+".0")
		}
	}
}

// The metadata that makes the binary and the installer look like a finished
// product rather than a downloaded tool: without it the Properties > Details
// tab is empty, which is the first thing a customer's IT department checks.
func TestInstallerCarriesVersionMetadata(t *testing.T) {
	src := readInstallerScript(t)

	for _, directive := range []string{
		"VersionInfoVersion=",
		"VersionInfoCompany=",
		"VersionInfoProductName=",
		"VersionInfoDescription=",
		"VersionInfoCopyright=",
		"UninstallDisplayIcon=",
		"SetupIconFile=",
	} {
		if !strings.Contains(src, directive) {
			t.Errorf("setup.iss no declara %s", directive)
		}
	}
}

// resolveInstallerDefines substitutes the "{#Name}" references so that a
// directive can be checked against the value it will really carry.
func resolveInstallerDefines(src string) string {
	resolved := src
	for _, m := range regexp.MustCompile(`(?m)^#define\s+(\w+)\s+"([^"]*)"`).FindAllStringSubmatch(src, -1) {
		resolved = strings.ReplaceAll(resolved, "{#"+m[1]+"}", m[2])
	}
	return resolved
}

// installerCodeSection returns the [Code] section of the script.
func installerCodeSection(t *testing.T, src string) string {
	t.Helper()

	index := strings.Index(src, "\n[Code]")
	if index < 0 {
		t.Fatal("setup.iss no tiene sección [Code]")
	}
	return src[index:]
}

// stripPascalNoise removes comments and string literals, so that a word inside
// a comment ("...runs its own end...") is not counted as a keyword and the text
// of a message is not mistaken for a call.
func stripPascalNoise(code string) string {
	code = regexp.MustCompile(`//[^\n]*`).ReplaceAllString(code, "")
	code = regexp.MustCompile(`(?s)\{[^#][^}]*\}`).ReplaceAllString(code, "")
	return regexp.MustCompile(`'[^'\n]*'`).ReplaceAllString(code, "''")
}
