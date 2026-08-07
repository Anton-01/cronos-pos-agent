package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Ventana de bienvenida post-instalación.
//
// El instalador lanza el agente con --first-run al terminar la barra de
// progreso. El agente arranca con normalidad (bandeja + servidor HTTP) y además
// abre una ventana ligera que confirma al operador que la instalación ha ido
// bien. Sin ella la instalación termina sin ninguna señal visible: el agente es
// un binario -H=windowsgui que sólo deja un icono de 16 px en la bandeja, y el
// operador de la caja no tiene forma de saber si funcionó.
//
// La ventana se abre desde el propio proceso del agente y no desde un segundo
// ejecutable: killOrphanInstances() mata cualquier otra instancia de
// cronos-pos-agent.exe al arrancar, así que un proceso aparte sólo para la
// ventana moriría en cuanto el agente hiciera su self-healing.

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
