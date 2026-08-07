//go:build windows

package main

import _ "embed"

// Recursos gráficos embebidos en el ejecutable.
//
// Se embeben con //go:embed y NO se leen del disco a propósito: el binario se
// reubica solo a C:\Program Files\CronosAgent y se sobrescribe en cada
// actualización (ver EnsurePermanentLocation). Un icono que viviera como
// archivo suelto junto al .exe se perdería en la primera actualización o al
// copiar el binario a mano, y el agente aparecería en la bandeja con el icono
// genérico de Windows. Embebido, el ejecutable es autónomo.

// appIconICO es el icono del System Tray, de la ventana de bienvenida y del
// propio instalador: un gato tuxedo blanco y negro. El .ico contiene siete
// resoluciones (16 a 256 px) para que Windows elija la correcta en la bandeja,
// en el Explorador y en Alt+Tab. Windows exige formato .ico: no acepta PNG.
//
//go:embed app_icon.ico
var appIconICO []byte

// welcomeCatPNG es la ilustración de la ventana de bienvenida: el gato tuxedo
// jugando con la ticketera. Va a 880×440 (el doble del tamaño con el que se
// dibuja) para verse nítida en pantallas HiDPI.
//
//go:embed welcome_cat.png
var welcomeCatPNG []byte

// trayIcon devuelve los bytes del icono en el formato que exige el System Tray
// de esta plataforma (.ico en Windows).
func trayIcon() []byte { return appIconICO }
