//go:build darwin

package main

import _ "embed"

// appIconPNG es el mismo gato tuxedo que app_icon.ico, en PNG de 64×64.
// La barra de menús de macOS se dibuja con NSImage (systray_darwin.m), que
// admite PNG de forma fiable pero no .ico, y reescala la imagen a 16 pt.
//
//go:embed app_icon.png
var appIconPNG []byte

// trayIcon devuelve los bytes del icono en el formato que exige el System Tray
// de esta plataforma (PNG en macOS).
func trayIcon() []byte { return appIconPNG }
