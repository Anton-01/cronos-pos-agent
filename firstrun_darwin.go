//go:build darwin

package main

import "os/exec"

// showWelcomeWindow muestra en macOS el equivalente ligero de la ventana de
// bienvenida de Windows: un diálogo del sistema con osascript.
//
// No se replica la ventana con ilustración porque el flujo que la motiva es
// específico de Windows: allí el instalador termina sin dejar ninguna señal
// visible salvo un icono de 16 px en la bandeja. En macOS la app se instala
// arrastrándola a /Applications y el propio Finder da esa confirmación.
func showWelcomeWindow() error {
	script := `display dialog "` + welcomeTitleMac + `" with title "Cronos POS Agent" buttons {"Cerrar"} default button "Cerrar" with icon note`
	return exec.Command("osascript", "-e", script).Run()
}

const welcomeTitleMac = "¡Cronos POS se ha instalado correctamente!"
