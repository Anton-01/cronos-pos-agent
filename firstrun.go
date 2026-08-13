package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Post-installation welcome dialog.
//
// The installer launches the agent with --first-run once the progress bar is
// done. The agent starts as usual (tray + HTTP server) and, on top of that,
// confirms to the operator that the installation went through. Without that
// confirmation the installation ends with no visible sign at all: the agent is
// a -H=windowsgui binary whose only trace is a 16 px icon in the tray, and the
// person standing at the till has no way of telling whether it worked.
//
// The dialog is opened by the agent's own process and not by a second
// executable: killOrphanInstances() kills every other cronos-pos-agent.exe on
// startup, so a separate process just for the dialog would die as soon as the
// agent ran its self-healing.
//
// The platform files provide showWelcomeWindow(): a native MessageBoxW on
// Windows (firstrun_windows.go) and an osascript dialog on macOS
// (firstrun_darwin.go). Neither adds a dependency, and neither can fail
// silently — both report an error that this file writes to the log.

// welcomeMarkerName es el archivo del directorio de datos que recuerda que la
// bienvenida ya se mostró. Guarda la versión del agente, de modo que una
// actualización a una versión nueva vuelve a confirmarse al operador, pero
// relanzar el mismo binario con --first-run no repite la ventana.
const welcomeMarkerName = "welcome-shown"

// ShowFirstRunWelcome muestra la ventana de bienvenida si corresponde. Bloquea
// hasta que el usuario la cierra, así que se invoca en su propia goroutine.
func ShowFirstRunWelcome() {
	if welcomeAlreadyShown() {
		log.Printf("[first-run] La bienvenida de la v%s ya se mostró, se omite", AgentVersion)
		return
	}

	// El marcador se escribe ANTES de abrir la ventana: si el subsistema
	// gráfico fallara, el agente no debe quedarse intentando abrirla en cada
	// arranque.
	markWelcomeShown()

	log.Println("[first-run] Mostrando la ventana de bienvenida")
	if err := showWelcomeWindow(); err != nil {
		log.Printf("[first-run] No se pudo mostrar la ventana de bienvenida: %v", err)
		return
	}
	log.Println("[first-run] Ventana de bienvenida cerrada")
}

func welcomeMarkerPath() string {
	return filepath.Join(agentDir(), welcomeMarkerName)
}

func welcomeAlreadyShown() bool {
	data, err := os.ReadFile(welcomeMarkerPath())
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == AgentVersion
}

func markWelcomeShown() {
	if err := os.WriteFile(welcomeMarkerPath(), []byte(AgentVersion), 0o600); err != nil {
		log.Printf("[first-run] No se pudo escribir el marcador de bienvenida: %v", err)
	}
}
