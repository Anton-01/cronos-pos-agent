//go:build darwin

package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// agentDir devuelve el directorio de datos del agente en macOS: config.json,
// logs y certificados conviven con el binario, como hasta ahora. A diferencia
// de Windows, macOS no tiene el problema de una ruta de instalación de sólo
// lectura para el usuario (una app en /Applications se instala arrastrándola y
// su carpeta de soporte es escribible).
func agentDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}

// EnsurePermanentLocation no hace nada en macOS: la reubicación forzosa del
// binario es específica de Windows, donde el agente se lanzaba desde carpetas
// volátiles (Descargas, %TEMP%) y el auto-arranque se rompía al desaparecer el
// archivo. En macOS el LaunchAgent se reescribe con la ruta real en cada
// arranque, que es la contramedida equivalente (ver EnsureAutostartRegistered).
func EnsurePermanentLocation() bool { return false }

// EnsureAutostartRegistered reescribe el LaunchAgent si falta o si su plist
// apunta a una ruta distinta a la del binario en ejecución (por ejemplo tras
// mover la app a /Applications). Respeta la preferencia guardada por el usuario.
func EnsureAutostartRegistered() {
	if !AutostartPreferred() {
		return
	}

	exePath, err := os.Executable()
	if err != nil {
		log.Printf("[autostart] No se pudo determinar la ruta del ejecutable: %v", err)
		return
	}

	if data, err := os.ReadFile(launchAgentPlistPath()); err == nil {
		if strings.Contains(string(data), "<string>"+exePath+"</string>") {
			return // el plist ya apunta al binario correcto
		}
		log.Printf("[autostart] LaunchAgent obsoleto, se reescribe con %s", exePath)
	}

	if err := enableAutostart(); err != nil {
		log.Printf("[autostart] No se pudo registrar el LaunchAgent: %v", err)
		return
	}
	log.Printf("[autostart] LaunchAgent registrado en %s", launchAgentPlistPath())
}
