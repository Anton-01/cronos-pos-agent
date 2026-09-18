package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Post-installation confirmation dialog.
//
// The installer launches the agent once the progress bar is done, passing the
// mode of the operation it just completed. The agent starts as usual (tray +
// HTTP server) and, on top of that, confirms to the operator that it went
// through — and whether what went through was a first installation or an
// upgrade, which are two different pieces of news: the first one has to explain
// where the agent now lives and what the cat in the tray is, the second only has
// to say that the till kept its pairing with the POS.
//
// Without that confirmation the installation ends with no visible sign at all:
// the agent is a -H=windowsgui binary whose only trace is a 16 px icon in the
// tray, and the person standing at the till has no way of telling whether it
// worked.
//
// The dialog is opened by the agent's own process and not by a second
// executable: killOrphanInstances() kills every other cronos-pos-agent.exe on
// startup, so a separate process just for the dialog would die as soon as the
// agent ran its self-healing. It is also the strongest confirmation available —
// a dialog painted by the agent proves the agent is running, which is the thing
// the operator actually needs to know and which the installer cannot vouch for.
//
// The platform files provide showSetupDialog(): a native MessageBoxW on Windows
// (firstrun_windows.go) and an osascript dialog on macOS (firstrun_darwin.go).
// Neither adds a dependency, and neither can fail silently — both report an
// error that this file writes to the log.

// Setup modes, as passed by the installer in --setup-mode.
const (
	setupModeInstall = "install"
	setupModeUpdate  = "update"
)

// welcomeMarkerName is the file in the data directory that remembers which
// installer run has already been confirmed.
//
// It holds the --setup-id the installer generated for that run, so a second
// process started from the same run —the relaunch from the permanent location,
// or a stray double invocation— finds the id already recorded and stays quiet,
// while the next run of the installer, which carries a different id, is
// confirmed again. That is what makes a repair install over the identical
// version still end with the dialog the operator is waiting for.
const welcomeMarkerName = "welcome-shown"

// SetupDialogRequest describes the confirmation the installer asked for.
type SetupDialogRequest struct {
	// Mode is setupModeInstall or setupModeUpdate. Anything else is treated as
	// an installation, which is the message that assumes the least.
	Mode string
	// RunID identifies the installer run. Empty falls back to the agent version,
	// which is what the pre-1.9.0 installers effectively used.
	RunID string
}

// ShowSetupDialog shows the post-installation dialog if this installer run has
// not been confirmed yet. It blocks until the operator closes it, so it is
// called from its own goroutine.
func ShowSetupDialog(req SetupDialogRequest) {
	marker := req.RunID
	if marker == "" {
		marker = AgentVersion + "|" + normalizeSetupMode(req.Mode)
	}

	if setupAlreadyConfirmed(marker) {
		log.Printf("[first-run] La confirmación de esta instalación (%s) ya se mostró, se omite", marker)
		return
	}

	// The marker is written BEFORE the dialog opens: if the graphics subsystem
	// failed, the agent must not be left trying to open it on every startup.
	markSetupConfirmed(marker)

	title, text := setupDialogText(normalizeSetupMode(req.Mode))

	log.Printf("[first-run] Mostrando la confirmación de %s", normalizeSetupMode(req.Mode))
	if err := showSetupDialog(title, text); err != nil {
		log.Printf("[first-run] No se pudo mostrar la confirmación: %v", err)
		return
	}
	log.Println("[first-run] Confirmación cerrada por el operador")
}

// normalizeSetupMode maps whatever the installer passed onto one of the two
// known modes. An unknown value becomes an installation: its message explains
// everything from scratch, which is never wrong, whereas the upgrade message
// would be telling a first-time operator that their settings were preserved
// from an installation that never existed.
func normalizeSetupMode(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), setupModeUpdate) {
		return setupModeUpdate
	}
	return setupModeInstall
}

// setupDialogText composes the title and body of the dialog for one mode.
//
// Both messages end on the same two facts —the agent is running now, and the cat
// in the tray is where to find it— because that is what the operator has to do
// next. What changes is everything before: a first install has to introduce the
// agent, while an upgrade has to answer the only question an upgrade raises,
// which is whether the till has to be paired with the POS again.
func setupDialogText(mode string) (title, text string) {
	if mode == setupModeUpdate {
		return fmt.Sprintf("Actualización completada — Cronos POS Agent v%s", AgentVersion),
			fmt.Sprintf(
				"El Agente de Impresión Cronos POS se actualizó correctamente a la versión %s.\n\n"+
					"Se conservaron el token de seguridad, los certificados y la configuración de tus impresoras, "+
					"así que no hace falta volver a vincular esta caja con el sistema de punto de venta.\n\n"+
					"El agente ya está en marcha. Puedes gestionarlo desde el icono del gatito en la barra de tareas, "+
					"junto al reloj.", AgentVersion)
	}

	return fmt.Sprintf("Instalación completada — Cronos POS Agent v%s", AgentVersion),
		fmt.Sprintf(
			"El Agente de Impresión Cronos POS se instaló correctamente (versión %s).\n\n"+
				"Se ejecuta en segundo plano y arranca solo con el sistema, así que no tienes que abrirlo cada día.\n\n"+
				"Puedes gestionarlo desde el icono del gatito en la barra de tareas, junto al reloj: desde ahí se copia "+
				"el token de seguridad y se imprime un ticket de prueba para comprobar la ticketera.", AgentVersion)
}

func welcomeMarkerPath() string {
	return filepath.Join(agentDir(), welcomeMarkerName)
}

func setupAlreadyConfirmed(marker string) bool {
	data, err := os.ReadFile(welcomeMarkerPath())
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == marker
}

func markSetupConfirmed(marker string) {
	if err := os.WriteFile(welcomeMarkerPath(), []byte(marker), 0o600); err != nil {
		log.Printf("[first-run] No se pudo escribir el marcador de confirmación: %v", err)
	}
}
