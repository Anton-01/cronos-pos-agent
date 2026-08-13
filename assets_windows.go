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

// appIconICO es el icono del System Tray y del propio instalador: un gato
// tuxedo blanco y negro. El .ico contiene siete resoluciones (16 a 256 px) para
// que Windows elija la correcta en la bandeja, en el Explorador y en Alt+Tab.
// Windows exige formato .ico: no acepta PNG.
//
// The welcome illustration (welcome_cat.png) is no longer embedded: the
// post-installation screen is now a native MessageBoxW, which renders its own
// window and cannot host a custom bitmap. The file stays in the repository as
// brand material and is still produced by tools/genassets.
//
//go:embed app_icon.ico
var appIconICO []byte

// appIconGrayICO es el gato en gris: el agente ha arrancado pero su servidor
// todavía no escucha. Estado neutro.
//
//go:embed app_icon_gray.ico
var appIconGrayICO []byte

// appIconGreenICO es el gato con el punto verde: el servidor HTTP está
// escuchando y el agente acepta trabajos de impresión. Estado operativo.
//
//go:embed app_icon_green.ico
var appIconGreenICO []byte

// trayIconStarting y trayIconReady devuelven los bytes del icono de cada estado
// en el formato que exige el System Tray de esta plataforma (.ico en Windows).
func trayIconStarting() []byte { return appIconGrayICO }
func trayIconReady() []byte    { return appIconGreenICO }
