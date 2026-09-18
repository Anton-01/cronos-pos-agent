//go:build darwin

package main

import (
	"os/exec"
	"strings"
)

// showSetupDialog muestra en macOS el equivalente ligero del diálogo de
// Windows: un cuadro del sistema con osascript.
//
// No se replica la ventana con ilustración porque el flujo que la motiva es
// específico de Windows: allí el instalador termina sin dejar ninguna señal
// visible salvo un icono de 16 px en la bandeja. En macOS la app se instala
// arrastrándola a /Applications y el propio Finder da esa confirmación.
//
// Las comillas dobles del texto se escapan antes de incrustarlo en el script:
// osascript recibe el diálogo como una cadena de AppleScript, y una comilla sin
// escapar la cerraría a mitad y convertiría el resto del mensaje en código.
func showSetupDialog(boxTitle, boxText string) error {
	script := "display dialog \"" + escapeAppleScript(boxText) + "\"" +
		" with title \"" + escapeAppleScript(boxTitle) + "\"" +
		" buttons {\"Cerrar\"} default button \"Cerrar\" with icon note"
	return exec.Command("osascript", "-e", script).Run()
}

// escapeAppleScript neutraliza las barras invertidas y las comillas dobles de
// un texto para poder incrustarlo en una cadena de AppleScript.
func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return strings.ReplaceAll(s, "\"", "\\\"")
}
