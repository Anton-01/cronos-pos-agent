//go:build darwin

package main

import _ "embed"

// El mismo gato tuxedo que app_icon.ico, en PNG de 64×64: la barra de menús de
// macOS se dibuja con NSImage (systray_darwin.m), que admite PNG de forma
// fiable pero no .ico, y reescala la imagen a 16 pt.
//
// Sólo se embeben las dos variantes de estado: la bandeja nunca muestra el
// icono base, que es el de marca (ejecutable, instalador y ventana).

// appIconGrayPNG es el estado neutro: el agente arrancó pero aún no escucha.
//
//go:embed app_icon_gray.png
var appIconGrayPNG []byte

// appIconGreenPNG es el estado operativo: el servidor HTTP ya escucha.
//
//go:embed app_icon_green.png
var appIconGreenPNG []byte

// trayIconStarting y trayIconReady devuelven los bytes del icono de cada estado
// en el formato que exige el System Tray de esta plataforma (PNG en macOS).
func trayIconStarting() []byte { return appIconGrayPNG }
func trayIconReady() []byte    { return appIconGreenPNG }
