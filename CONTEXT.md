# Cronos POS Agent — Contexto del Proyecto

## Estado Actual

**Fase 12: Instalador con Manejo Inteligente de Actualizaciones** — Finalizado

Fases completadas: 1 (Inicialización), 2 (Autodescubrimiento), 3 (Motor RAW ESC/POS), 4 (Seguridad, Autostart, Build), 5 (CORS dinámico, Health, Monitoreo de cola), 6 (Port fallback, Self-healing, Certificados SSL nativos, Instalador Inno Setup), 7 (Impresión nativa de PDF en impresoras convencionales), 8 (CREATE_NO_WINDOW anti-parpadeo, copiar token al portapapeles, autostart con ruta entre comillas), 9 (Ruta permanente en Program Files, auto-reubicación y reparación del registro, páginas de códigos ESC/POS con transcodificación de acentos), 10 (Página de códigos CP1252 por defecto, icono del gato tuxedo embebido, ventana de bienvenida post-instalación), 11 (Icono dinámico gris → verde ligado al socket, transcodificación con `golang.org/x/text/encoding/charmap`, cierre limpio del agente), 12 (Detección de instalación previa en `InitializeSetup`, pantalla de reinstalación, cierre preventivo del proceso activo y log de instalación para soporte).

## Arquitectura

### Decisiones Técnicas

| Decisión | Elección | Justificación |
|---|---|---|
| Lenguaje | Go 1.x | Binario estático, concurrencia nativa, cross-compile Windows/Mac |
| System tray | `github.com/getlantern/systray` v1.2.2 | API simple, soporte Windows/Mac/Linux |
| Servidor HTTP | `net/http` (stdlib) | Sin dependencias externas, rendimiento suficiente para agente local |
| CORS | Middleware dinámico desde `config.json` | Orígenes configurables sin recompilar |
| Binding | `127.0.0.1:{port}` | Solo loopback, puerto dinámico con fallback |
| Auth | Token local UUID v4 + header `X-Cronos-Agent-Token` | Sin servidor externo, generado al primer arranque |
| Certificados SSL | `crypto/rsa` + `crypto/x509` (stdlib) | Generación nativa sin OpenSSL ni comandos externos |
| Port Fallback | `net.Listen` + scan secuencial | Resiliencia ante conflictos de puerto |
| Self-healing | `tasklist`/`pgrep` + `os.Process.Kill` | Eliminación de instancias huérfanas |
| Printers (Win) | `github.com/alexbrainman/printer` | Acceso al Windows Print Spooler via syscall |
| Printers (Mac) | `lpstat -a` / `lp -d -o raw` (stdlib `os/exec`) | Descubrimiento e impresión CUPS nativa |
| PDF Print (Win) | `ShellExecuteW` via `syscall` + `shell32.dll` | Impresión silenciosa de PDF usando el verbo "print" del sistema |
| PDF Print (Mac) | `lp -d` (stdlib `os/exec`) | CUPS maneja PDF nativamente sin conversión |
| Cola (Win) | PowerShell `Get-PrintJob` | Lectura nativa del Spooler sin CGO |
| Ocultación de consola (Win) | `SysProcAttr{HideWindow, CreationFlags: CREATE_NO_WINDOW}` | Elimina el parpadeo de PowerShell/tasklist en todos los subprocesos |
| Copiar token (Systray) | `github.com/atotto/clipboard` | Portapapeles multiplataforma (pbcopy en macOS, API nativa en Windows) |
| Cola (Mac) | `lpstat -W not-completed -o` | Consulta CUPS nativa de trabajos pendientes |
| Build Tags | `//go:build windows` / `//go:build darwin` | Compilación condicional por plataforma |
| Autostart (Win) | Registro de Windows `HKCU\...\Run` con ruta entrecomillada | Estándar de Windows para apps de usuario |
| Autostart (Mac) | LaunchAgent plist en `~/Library/LaunchAgents` | Estándar de macOS para agentes de usuario |
| Ubicación del binario (Win) | Fija: `C:\Program Files\CronosAgent\` (o `C:\ProgramData\CronosAgent\` sin admin) | Modelo QZ Tray: ruta permanente que sobrevive a reinicios y limpiezas de disco |
| Auto-reubicación (Win) | Copia + relanzado con `DETACHED_PROCESS` | Un binario lanzado desde Descargas/`%TEMP%` se instala solo en la ruta permanente |
| Datos de runtime (Win) | `%LOCALAPPDATA%\CronosAgent\` | Program Files es de sólo lectura para el usuario estándar que ejecuta el agente |
| Codificación ESC/POS | `ESC t n` + transcodificación UTF-8 → página de códigos | Las ticketeras no entienden UTF-8; CP437 (fábrica) ni siquiera contiene Á Í Ó Ú |
| Página por defecto | **CP1252** (`ESC t 16` = `1B 74 10`) desde la v1.5.0 | Sus bytes son los de Latin-1, que es lo que espera una ticketera conectada a Windows. Con CP850 la `Á` viaja como `0xB5` y sale como otro símbolo en cuanto el hardware pierde la selección de página |
| Numeración de páginas | `escpos_code_page_id` en `config.json` | Válvula de escape para clones que numeran sus tablas fuera del estándar Epson, sin recompilar |
| Tablas de códigos | `golang.org/x/text/encoding/charmap` | Implementación de referencia del proyecto Go: ~800 líneas de tablas propias sustituidas por cuatro alias que nadie tiene que revisar |
| Transcodificación | `charmap.Windows1252.NewEncoder().Bytes()` por tramo de texto | Un ticket RAW no es una cadena: sólo se codifican los tramos de texto, no los comandos ni los logos |
| Icono del agente | `//go:embed app_icon.ico` + `systray.SetIcon` | El binario se sobrescribe en cada actualización: un icono en archivo suelto se perdería |
| Estado en la bandeja | Icono gris → verde tras `net.Listen` | El color confirma que el socket acepta conexiones, no que se haya lanzado una goroutine |
| Cierre del agente | `srv.Shutdown(ctx)` en `onExit` + canal `agentDone` | Libera el puerto y no corta un ticket a medio enviar al spooler |
| Recursos Win32 | `rsrc_windows_amd64.syso` (icono + manifiesto) | Icono en Explorador/Alt+Tab y botones con estilo moderno (Common Controls 6) |
| Ventana de bienvenida | Win32 nativo (`user32`/`gdi32` vía `syscall`) | Cero dependencias nuevas; `lxn/win` está sin mantenimiento y `fyne` exige CGO + OpenGL |
| Ilustraciones | Generadas por código (`tools/genassets`) | Recursos reproducibles y auditables en vez de binarios opacos |
| Logs | Rotación nativa con `RotatingLogger` | Sin dependencias externas, 10MB max, 3 backups |
| Updates | Goroutine con polling cada 6h | Consulta JSON remoto |
| Instalador | Inno Setup 6.3+ | Instalador silencioso estándar de Windows; pide admin para Program Files y cae a ProgramData si no lo hay |
| Detección de reinstalación | Clave `Uninstall\{AppId}_is1` leída en `InitializeSetup()` | Es la clave que el propio Inno escribe al instalar: no hay que inventar un marcador propio que se pueda desincronizar |
| Pantalla de reinstalación | `CreateCustomForm` (Pascal Scripting) | Un `MsgBox` no permite fijar el título de la ventana ni maquetar la lista de lo que se conserva |
| Cierre del proceso activo | `taskkill /T` y, si sobrevive, `/F /T` desde `PrepareToInstall()` | Windows bloquea la sobrescritura de un `.exe` en uso; el primer intento deja que el agente ejecute su `onExit()` |
| Log del instalador | `SetupLogging=yes` + copia en `{app}\install-log.txt` | Soporte pide siempre la misma ruta en vez de buscar un archivo con la fecha en el `%TEMP%` del operador |

### Estructura de Archivos

```
cronos-pos-agent/
├── main.go              # Entry point: flags CLI, self-healing, reubicación, systray, goroutines
├── server.go            # Router, middlewares (CORS dinámico + Auth), handlers (6 endpoints)
├── config.go            # Carga/generación de config.json, AgentVersion (1.6.0), migraciones de esquema
├── network.go           # ResolvePort: fallback dinámico de puertos con scan
├── certs.go             # GenerateCerts: RSA 2048 + X.509 autofirmado nativo
├── logger.go            # RotatingLogger: escritura a archivo con rotación 10MB/3 backups
├── updater.go           # CheckForUpdates: polling de versión contra servidor central
├── printer.go           # Tipos compartidos (PrinterInfo, PrintRequest, QueueInfo, PrintJob)
├── escpos.go            # Motor de codificación: ESC t n, encoder charmap y salto de gráficos
├── escpos_codepages.go  # Alias de charmap (CP1252/CP850/CP858/CP437) + fallback ASCII
├── escpos_test.go       # Tests del motor de codificación (17 casos)
├── paths_windows.go     # Build tag: windows — ruta permanente, reubicación, directorio de datos
├── paths_darwin.go      # Build tag: darwin — directorio de datos y reparación del LaunchAgent
├── printer_windows.go   # Build tag: windows — spooler, RAW, cola, autostart, killOrphan
├── printer_darwin.go    # Build tag: darwin — CUPS, RAW, cola, autostart, killOrphan
├── assets_windows.go    # Build tag: windows — //go:embed del .ico y de la ilustración
├── assets_darwin.go     # Build tag: darwin — //go:embed del icono PNG de la barra de menús
├── firstrun.go          # Marcador de "bienvenida ya mostrada" y orquestación de --first-run
├── firstrun_windows.go  # Build tag: windows — ventana de bienvenida nativa Win32
├── firstrun_darwin.go   # Build tag: darwin — diálogo equivalente con osascript
├── app_icon.ico         # Icono del gato tuxedo, 7 resoluciones (16–256 px) — embebido
├── app_icon.png         # El mismo icono en PNG 64×64 (macOS / material de marca)
├── app_icon_gray.ico    # Estado "iniciando": gato gris — embebido (System Tray)
├── app_icon_gray.png    # Ídem para la barra de menús de macOS
├── app_icon_green.ico   # Estado "operativo": gato con punto verde — embebido
├── app_icon_green.png   # Ídem para la barra de menús de macOS
├── welcome_cat.png      # Ilustración 880×440: el gato jugando con la ticketera — embebida
├── app.manifest         # Manifiesto Win32: asInvoker + Common Controls 6.0
├── rsrc_windows_amd64.syso # Recurso Win32 generado (icono + manifiesto) que enlaza el .exe
├── tools/
│   └── genassets/
│       └── main.go      # Generador de los 3 iconos y de welcome_cat.png (dibujo por código)
├── installer/
│   └── setup.iss        # Script Inno Setup: instalador silencioso + lógica de actualización/reinstalación
├── .gitignore
├── go.mod
├── go.sum
├── CONTEXT.md           # Este archivo — fuente de verdad del proyecto
└── README.md
```

### Archivos generados en runtime (fuera del repositorio)

Desde la Fase 9 los datos ya **no** viven junto al binario en Windows, porque
`C:\Program Files\CronosAgent\` es de sólo lectura para el usuario estándar que
ejecuta el agente. Ver "Ubicación Permanente del Binario".

| Archivo | Windows | macOS |
|---|---|---|
| `config.json` | `%LOCALAPPDATA%\CronosAgent\` | Junto al binario |
| `cronos-agent.log` (+ `.1`–`.3`) | `%LOCALAPPDATA%\CronosAgent\` | Junto al binario |
| `private-key.pem` | `%LOCALAPPDATA%\CronosAgent\` | Junto al binario |
| `digital-certificate.txt` | `%LOCALAPPDATA%\CronosAgent\` | Junto al binario |
| `welcome-shown` | `%LOCALAPPDATA%\CronosAgent\` | Junto al binario |

## Archivo `config.json` — Esquema Completo

```json
{
  "config_version": 2,
  "api_token": "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d",
  "allowed_origins": [
    "https://pos-app.tech",
    "http://localhost:3000",
    "http://localhost:5173",
    "http://127.0.0.1:3000",
    "http://127.0.0.1:5173"
  ],
  "update_url": "https://pos-app.tech/agent/version.json",
  "port": 9100,
  "escpos_code_page": "cp1252",
  "escpos_transcode": true,
  "autostart": true
}
```

| Propiedad | Tipo | Default | Descripción |
|---|---|---|---|
| `config_version` | `int` | `2` | Versión del **esquema** del archivo (no la del agente). Dispara las migraciones una sola vez |
| `api_token` | `string` | UUID v4 auto | Token de autenticación para header `X-Cronos-Agent-Token` |
| `allowed_origins` | `string[]` | 5 orígenes | Lista de orígenes CORS permitidos |
| `update_url` | `string` | pos-app.tech | URL del JSON de versión para auto-updates |
| `port` | `int` | `9100` | Puerto preferido. Si está ocupado, busca el siguiente libre (9101–9110) |
| `escpos_code_page` | `string` | `"cp1252"` | Página de códigos que se activa en la ticketera: `cp1252`, `cp850`, `cp858`, `cp437` o `none` |
| `escpos_transcode` | `bool` | `true` | Convierte el texto UTF-8 a los bytes de esa página de códigos |
| `escpos_code_page_id` | `int` | ausente | Sustituye el `n` de `ESC t n` por un valor concreto (0–255) manteniendo la tabla de `escpos_code_page`. Sólo para ticketeras con numeración propia |
| `autostart` | `bool` | `true` | Preferencia de arranque con el sistema. El agente sólo repara la entrada del registro si es `true` |

Las claves nuevas se añaden automáticamente al `config.json` existente en el
primer arranque, sin perder el `api_token` ya emitido al frontend.

### Migraciones de esquema (`config_version`)

`LoadConfig()` compara el `config_version` del archivo con el que espera esta
versión del agente y aplica las migraciones pendientes una sola vez, dejando
intacto el `api_token`.

| Migración | Qué hace | Por qué |
|---|---|---|
| v1 → v2 | `escpos_code_page: "cp850"` → `"cp1252"` | Ese `cp850` no lo eligió ningún operador: lo escribió el propio agente como valor por defecto en su primer arranque. Sin la migración, las cajas ya instaladas seguirían imprimiendo los acentos mal tras actualizar |

Una página **distinta** de `cp850` (por ejemplo `cp858`, `cp437` o `none`) sí es
una decisión deliberada del integrador y se respeta.

## Conmutación Dinámica de Puertos

Al arrancar, `ResolvePort()` ejecuta la siguiente cascada:

1. Intenta el puerto del `config.json` (`port` field)
2. Si está ocupado, intenta el puerto por defecto `9100`
3. Si ambos fallan, escanea secuencialmente `9101` → `9110`
4. Si los 10 puertos están ocupados, el agente sale con error fatal

La detección usa `net.Listen("tcp", "127.0.0.1:PORT")` — si el bind falla, el puerto está ocupado. El systray muestra el puerto activo en el tooltip y en el menú de estado.

## Self-Healing: Detección de Instancias Huérfanas

Antes de arrancar el servidor HTTP, `killOrphanInstances()` ejecuta:

| Plataforma | Comando | Lógica |
|---|---|---|
| Windows | `tasklist /FI "IMAGENAME eq cronos-pos-agent.exe" /FO CSV /NH` | Parsea PIDs del CSV, mata todos excepto el PID actual |
| macOS | `pgrep -f cronos-pos-agent` | Lista PIDs que coincidan, mata todos excepto el actual |

Esto previene instancias zombie que bloqueen puertos o consuman RAM.

## Ubicación Permanente del Binario (Estilo QZ Tray)

### El problema en producción

El agente se rompía tras cada reinicio de la caja de cobro. La causa: el binario
se ejecutaba desde donde el operador lo hubiera descargado —`Descargas`, el
Escritorio, `%TEMP%`, un pendrive— y la entrada de auto-arranque del registro
apuntaba a esa misma ruta volátil. Al vaciar Descargas, mover el archivo o
limpiar el disco, la entrada quedaba apuntando a un archivo inexistente y
Windows la ignoraba en silencio: ningún error, ningún icono en el System Tray.

### La solución: ruta fija y obligatoria

Igual que QZ Tray, el binario vive **siempre** en una ruta fija del sistema:

| Prioridad | Ruta | Cuándo |
|---|---|---|
| 1 | `C:\Program Files\CronosAgent\cronos-pos-agent.exe` | Instalación con privilegios de administrador |
| 2 | `C:\ProgramData\CronosAgent\cronos-pos-agent.exe` | Sin elevación: permanente y escribible sin admin |
| 3 | `%LOCALAPPDATA%\CronosAgent\cronos-pos-agent.exe` | Último recurso por usuario (y ruta de instalaciones ≤ 1.3.0) |

Las tres se consideran ubicaciones permanentes válidas (`isPermanentLocation`),
de modo que un agente ya instalado en ProgramData **no** se reubica a Program
Files en cada arranque. La preferencia sólo se aplica al elegir destino nuevo.

### Auto-reubicación al arrancar (`EnsurePermanentLocation`)

Definida en `paths_windows.go` y ejecutada desde `main()` justo después del
self-healing. Si el binario **no** está en una ruta permanente:

1. Elige la primera raíz realmente escribible (`writableInstallTarget`), lo que
   se comprueba creando un archivo de prueba: que `MkdirAll` funcione no
   garantiza permiso de escritura dentro de la carpeta.
2. Copia el ejecutable a la ruta definitiva (`replaceExecutable`). La copia es
   atómica —archivo temporal + `os.Rename`— para no dejar nunca un `.exe` a
   medio escribir. Si el destino está bloqueado por un proceso vivo, se renombra
   el binario antiguo a `.old` (Windows sí permite renombrar un ejecutable en
   uso) y se reintenta.
3. Migra `config.json`, `private-key.pem` y `digital-certificate.txt` desde la
   carpeta de origen (`migrateRuntimeData`). **Crítico**: sin esto se regeneraría
   el `api_token` y habría que reconfigurar cada caja de cobro.
4. Relanza el binario definitivo con `--relaunched`, `CREATE_NO_WINDOW` y
   `DETACHED_PROCESS`, y la instancia temporal termina.

El flag `--relaunched` corta cualquier posibilidad de bucle de reubicación.
`--no-install` desactiva el mecanismo completo para desarrollo local.

### Separación binario / datos

Program Files es de sólo lectura para un usuario estándar, pero el agente
escribe su `config.json` y sus logs en cada arranque. Por eso `agentDir()` en
Windows ya no devuelve la carpeta del ejecutable sino `%LOCALAPPDATA%\CronosAgent`.

Esa carpeta coincide con el `DefaultDirName` de las versiones ≤ 1.3.0, así que
al actualizar desde una instalación anterior el `config.json` (y con él el token
ya entregado al frontend) **queda donde estaba**: no hay migración que hacer.

## Auto-arranque a Prueba de Reinicios (Windows)

### Ruta entre comillas dobles — obligatorio

El valor escrito en `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` va
**siempre** encerrado entre comillas dobles:

```go
// quotedRegistryPath encierra la ruta tal y como exige el registro de Windows.
func quotedRegistryPath(exePath string) string {
    return fmt.Sprintf(`"%s"`, exePath)
}
// -> "C:\Program Files\CronosAgent\cronos-pos-agent.exe"
```

Sin las comillas Windows trunca el comando en el primer espacio: `C:\Program
Files\CronosAgent\cronos-pos-agent.exe` se interpreta como `C:\Program` con dos
argumentos, no encuentra el ejecutable y descarta la entrada sin avisar. Con la
ruta ahora en `Program Files` —que contiene un espacio por definición— las
comillas dejan de ser una precaución para ser un requisito estricto.

### Qué ruta se registra (`autostartTargetPath`)

Nunca la del proceso en curso: siempre la **ubicación permanente**. Si el agente
se está ejecutando desde una carpeta volátil, se registra la copia instalada.
Registrar la ruta temporal es exactamente lo que rompía el arranque.

### Reparación en cada arranque (`EnsureAutostartRegistered`)

`main()` la invoca en todos los arranques. Lee el valor actual y lo reescribe si
falta, si apunta a otra ruta o si le faltan las comillas —el caso de las
entradas dejadas por instaladores anteriores—. Así el auto-arranque se
autorrepara tras una actualización del agente, un cambio de ubicación o una
actualización del sistema operativo.

Respeta la preferencia del usuario: si desmarcó **"Iniciar con el Sistema"** en
el System Tray, la opción queda guardada en `config.json` (`"autostart": false`)
y la entrada no se vuelve a crear.

En macOS el equivalente reescribe el `LaunchAgent` cuando su `plist` apunta a una
ruta distinta de la del binario en ejecución (por ejemplo tras mover la app a
`/Applications`).

## Flags de Línea de Comandos

| Flag | Descripción |
|---|---|
| `--generate-certs` | Genera `private-key.pem` y `digital-certificate.txt` en el directorio de datos y sale |
| `--disable-autostart` | Elimina el auto-arranque y guarda la preferencia. Lo usa el desinstalador |
| `--no-install` | No reubica el binario a la ruta permanente (uso en desarrollo) |
| `--relaunched` | Uso interno: marca la instancia ya relanzada desde la ruta permanente |
| `--first-run` | Arranca con normalidad y además abre la ventana de bienvenida. Lo usa el instalador al terminar la barra de progreso |
| (sin flags) | Modo normal: self-healing, reubicación, reparación de autostart, systray + servidor HTTP |

### Generación de Certificados SSL

Ejecutar: `cronos-pos-agent.exe --generate-certs`

Genera usando paquetes estándar de Go (cero dependencias externas):
- **`private-key.pem`**: Llave privada RSA 2048-bit (permisos `0600`)
- **`digital-certificate.txt`**: Certificado X.509 autofirmado PEM

| Parámetro del Certificado | Valor |
|---|---|
| Algoritmo | RSA 2048-bit |
| Validez | 10 años desde la fecha de generación |
| Subject CN | `localhost` |
| Organization | `Cronos POS Agent` |
| SAN (Subject Alternative Names) | `localhost`, `127.0.0.1` |
| Key Usage | KeyEncipherment, DigitalSignature |
| Extended Key Usage | ServerAuth |

Paquetes Go utilizados: `crypto/rsa`, `crypto/x509`, `crypto/x509/pkix`, `crypto/rand`, `encoding/pem`, `math/big`.

## Instalador Silencioso Windows — Inno Setup

**Herramienta recomendada:** [Inno Setup 6.3+](https://jrsoftware.org/isinfo.php) — gratuito, ligero, estándar de la industria para apps de escritorio Windows. Elegido sobre NSIS (sintaxis más compleja) y WiX (sobredimensionado para un agente single-binary). Se requiere 6.3 o superior por `ArchitecturesInstallIn64BitMode=x64compatible`, necesario para que `{commonpf}` resuelva a `C:\Program Files` y no a `Program Files (x86)`.

### Compilar el instalador

```bash
# 1. Compilar el binario optimizado
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
  go build -ldflags="-H=windowsgui -w -s" -o build/cronos-pos-agent.exe .

# 2. Generar el instalador (desde Windows con Inno Setup instalado)
ISCC.exe installer/setup.iss
```

### Comportamiento del instalador

| Paso | Acción | Detalle |
|---|---|---|
| 1 | Detecta la instalación previa | `InitializeSetup()` lee `Uninstall\{AppId}_is1` y, si existe, muestra la pantalla de actualización (ver "Actualización y Reinstalación") |
| 2 | Elige el destino | La ruta de la instalación anterior si la hay; si no, `C:\Program Files\CronosAgent\` con admin y `C:\ProgramData\CronosAgent\` sin elevación (`PermanentInstallDir`) |
| 3 | Cierra el agente en memoria | `taskkill /T` y, si sobrevive, `/F /T` desde `PrepareToInstall()`, justo antes de la copia de archivos |
| 4 | Copia el binario a la ruta permanente | Sobrescritura garantizada: en el paso anterior se ha comprobado que ya no hay ningún proceso reteniendo el `.exe` |
| 5 | Genera certificados SSL | `--generate-certs` en modo oculto y con `runasoriginaluser`. **Sólo en instalación nueva** (`Check: not IsUpgradeInstall`) |
| 6 | Registra el autostart | `HKCU\...\Run` → `CronosPOSAgent` con la ruta **entre comillas dobles** |
| 7 | Lanza el agente | En segundo plano y con `runasoriginaluser`. En instalación atendida con `--first-run` y **sin** `runhidden`, para que se vea la ventana de bienvenida; en `/VERYSILENT`, sin el flag y con `runhidden` |
| 8 | Deja el log para soporte | Copia el registro de `SetupLogging=yes` a `{app}\install-log.txt` en `CurStepChanged(ssDone)` |

El instalador usa el mismo gato tuxedo como icono (`SetupIconFile=..\app_icon.ico`),
y "Aplicaciones instaladas" lo muestra a través del recurso Win32 del propio
ejecutable (`UninstallDisplayIcon={app}\cronos-pos-agent.exe`).

**`PrivilegesRequired=admin` + `PrivilegesRequiredOverridesAllowed=dialog commandline`:**
se pide elevación para instalar en Program Files; si no hay credenciales de
administrador, Inno reintenta sin elevar y el destino cae a `C:\ProgramData\CronosAgent`,
que también es permanente y escribible sin admin. El despliegue silencioso sigue
funcionando en ambos casos.

**`runasoriginaluser` es imprescindible** en los pasos 5 y 7: cuando el
instalador corre elevado, `HKCU` y `%LOCALAPPDATA%` son los del administrador y
no los del operador de la caja. Ejecutando el agente como el usuario original,
el token, los certificados y la clave de auto-arranque acaban en el perfil
correcto. Por el mismo motivo la sección `[Registry]` lleva
`Check: not IsAdminInstallMode`: en instalaciones elevadas es el propio agente
quien registra el auto-arranque en la rama correcta durante su primer arranque
(`EnsureAutostartRegistered`).

### Instalación silenciosa por línea de comandos

```bash
CronosAgentSetup-1.6.0.exe /VERYSILENT /SUPPRESSMSGBOXES /NORESTART
```

- `/VERYSILENT`: Sin interfaz gráfica
- `/SUPPRESSMSGBOXES`: Sin diálogos de confirmación
- `/NORESTART`: No reiniciar Windows

En modo silencioso la pantalla de reinstalación **no** se muestra (`WizardSilent`):
un despliegue masivo se quedaría colgado esperando un clic que nadie va a dar.
La detección, el cierre del proceso y el log sí se ejecutan igual.

## Actualización y Reinstalación del Instalador

Hasta la v1.6.0 el instalador trataba una actualización exactamente igual que una
instalación limpia: no avisaba de nada, podía elegir un destino distinto al de la
instalación previa y sobrescribía los certificados. Y si el operador tenía el
agente abierto —que es lo normal, arranca con Windows—, la copia del `.exe`
fallaba con el clásico "el archivo está en uso".

### 1. Detección de la instalación previa (`InitializeSetup`)

La fuente de verdad es la clave que **el propio Inno Setup** escribe al instalar,
compuesta con el `AppId` y el sufijo `_is1`:

```
Software\Microsoft\Windows\CurrentVersion\Uninstall\{B7E3F4A2-9C1D-4E5F-A8B6-7D2C3E4F5A6B}_is1
```

De ahí se leen `DisplayVersion` (la versión instalada) e `Inno Setup: App Path`
(la carpeta real del binario, con `InstallLocation` como reserva).

**Se consultan cuatro ramas, no una.** El agente se instala con elevación o sin
ella, y cada modalidad deja la clave en un sitio distinto:

| Orden | Rama | Cuándo la escribió |
|---|---|---|
| 1 | `HKLM` | Instalación elevada (destino `Program Files`) |
| 2 | `HKCU` | Instalación sin elevación (destino `ProgramData`) |
| 3 | `HKLM32` | Instalador de 32 bits anterior a `ArchitecturesInstallIn64BitMode` (vista `WOW6432Node`) |
| 4 | `HKCU32` | Ídem sin elevación |

Buscar sólo en la rama que usaría *esta* ejecución haría que un instalador
elevado no viera la instalación que el operador hizo sin permisos de
administrador, y acabaría con dos copias del agente en el equipo.

El GUID se declara una sola vez (`#define AppGuid`) y se reutiliza tanto en
`AppId` como en la ruta de la clave, para que no puedan divergir.

### 2. Pantalla de reinstalación

Si la detección acierta y la instalación es atendida, antes de tocar un solo
archivo se abre una ventana propia (`CreateCustomForm`) con:

| Elemento | Contenido |
|---|---|
| Título de la ventana | **"Actualización / Reinstalación detectada de Cronos POS Agent"** |
| Encabezado | El mismo texto, en negrita y tres puntos más grande |
| Mensaje | "Hemos detectado que Cronos POS Agent ya se encuentra instalado en este equipo. El proceso actualizará el motor del agente y los recursos visuales, manteniendo intactas tus configuraciones y tokens de seguridad actuales para que no tengas que reconfigurar nada." |
| Lista de lo que se conserva | `api_token`, `config.json` (puerto, CORS, página de códigos), certificados SSL y la preferencia de "Iniciar con el Sistema" |
| Datos detectados | Versión previa y ruta encontradas en el registro, en gris, más la versión que se va a instalar |
| Botones | **"Actualizar ahora"** (por defecto) y **"Cancelar"** |

Se usa un formulario y no un `MsgBox` porque el requisito incluye el **título de
la ventana**, y `MsgBox` hereda siempre el del instalador. La ventana se centra
en pantalla y todas las medidas pasan por `ScaleX`/`ScaleY`, así que se ve igual
en una caja de cobro al 100 % que en un portátil al 150 %.

Cancelar devuelve `False` desde `InitializeSetup()` y el instalador termina sin
haber modificado nada.

### 3. Qué se conserva de verdad en una actualización

La pantalla promete que no hay que reconfigurar nada. Eso obliga a tres cosas
concretas en el script:

| Recurso | Por qué sobrevive |
|---|---|
| `config.json` (con el `api_token`) | Vive en `%LOCALAPPDATA%\CronosAgent\`, que el instalador sólo toca al desinstalar. `LoadConfig()` añade las claves nuevas sin tocar el token |
| Certificados SSL | El paso `--generate-certs` lleva `Check: not IsUpgradeInstall`. `GenerateCerts()` abre los archivos con `O_TRUNC`: ejecutarlo en cada actualización emitiría un par RSA nuevo y tumbaría el certificado que el frontend pudiera tener ya aceptado |
| Ruta de instalación | `PermanentInstallDir` devuelve la carpeta de la instalación anterior si existe, y `UsePreviousAppDir=yes` lo refuerza. Mover el binario de `ProgramData` a `Program Files` en una actualización dejaría dos copias y una entrada de auto-arranque apuntando a la que ya no se actualiza |
| Preferencia de auto-arranque | La escribe el agente en `config.json`; el instalador no la fuerza (`Check: not IsAdminInstallMode` en `[Registry]`) |

La ventana de bienvenida **sí** vuelve a aparecer al actualizar: el marcador
`welcome-shown` guarda la versión que ya se mostró (ver "Ventana de Bienvenida
Post-Instalación"), y confirmarle al operador que la actualización ha ido bien es
justamente lo que se quiere.

### 4. Cierre preventivo del proceso activo

`PrepareToInstall()` se ejecuta **inmediatamente antes** de la copia de archivos,
que es el único punto donde la comprobación sirve de algo.

```
AgentIsRunning?  ──no──>  seguir
      │ sí
      ▼
taskkill /T /IM cronos-pos-agent.exe        (cierre ordenado, sin /F)
      │  espera hasta 4 s, sondeando cada 250 ms
      ▼
¿sigue vivo? ──sí──>  taskkill /F /T /IM …  (forzado, espera hasta 6 s)
      │ no
      ▼
binario libre: Inno puede sobrescribirlo
```

**Por qué dos pasos y no un `/F` directo.** Un `taskkill` sin `/F` manda
`WM_CLOSE`, lo que deja al agente ejecutar su `onExit()`: cierra el servidor con
`srv.Shutdown(ctx)` —sin cortar un ticket a medio camino del spooler— y libera el
puerto 9100. `/F` termina el proceso en seco. Se intenta lo limpio primero y sólo
se fuerza a quien no responde.

**Por qué `/T`.** Arrastra a los procesos hijos: el `powershell` de `Get-PrintJob`
puede seguir vivo con un handle abierto sobre el directorio del agente.

**Cómo se sabe si sigue corriendo.** `taskkill` es asíncrono, así que no basta
con lanzarlo: se sondea la lista de procesos hasta que desaparece.

```
cmd /C tasklist /FI "IMAGENAME eq cronos-pos-agent.exe" /NH | find /I "cronos-pos-agent.exe" > nul
```

`tasklist` devuelve `0` aunque no encuentre nada, así que la respuesta la da
`find`, que devuelve `1` si la línea no aparece. Todo con `SW_HIDE`: ninguna
ventana de consola parpadea en la caja de cobro.

**Si aun así sobrevive**, `PrepareToInstall()` devuelve un mensaje y la
instalación se aborta antes de empezar. Es preferible a fallar a mitad de la
copia y dejar un `.exe` a medio escribir:

> No se ha podido cerrar Cronos POS Agent, que sigue en ejecución. Ciérralo desde
> el icono del gato en la bandeja del sistema (menú "Salir") y vuelve a ejecutar
> el instalador.

Si el `Exec` de `tasklist` fallara por lo que sea, la función responde "no está
corriendo" y la instalación continúa: debajo sigue estando `CloseApplications=force`,
que usa el Restart Manager de Windows. Se falla hacia el lado que no bloquea al
operador.

### 5. Registro de instalación para soporte técnico

`SetupLogging=yes` deja **siempre** —también en `/VERYSILENT`— un log detallado
en `%TEMP%\Setup Log AAAA-MM-DD #NNN.txt` con cada archivo copiado, cada clave de
registro tocada y el motivo exacto de cualquier fallo.

Además, `CurStepChanged(ssDone)` copia ese log a una ruta fija y predecible:

```
C:\Program Files\CronosAgent\install-log.txt
```

Soporte pide siempre el mismo archivo en vez de hacer que el operador busque uno
con la fecha en el nombre dentro de su carpeta temporal. Un administrador puede
además fijar la ruta desde la línea de comandos:

```bash
CronosAgentSetup-1.6.0.exe /VERYSILENT /LOG="C:\soporte\cronos-install.log"
```

El script escribe sus propias trazas en ese mismo log con el prefijo `[Cronos]`
(versión previa detectada, ruta reutilizada, cierre ordenado o forzado del
proceso, cancelación desde la pantalla). Si la instalación se aborta antes del
final, `ssDone` no llega a ejecutarse y la copia en `{app}` no existe: en ese caso
el log válido es el de `%TEMP%`, cuya ruta muestra el propio Inno en el error.

El desinstalador borra `install-log.txt` junto con el resto de archivos.

### `setup.iss` se guarda en UTF-8 **con BOM**

Inno Setup 6 lee un `.iss` sin BOM usando la página ANSI del sistema donde se
compila. Mientras los acentos vivían sólo en los comentarios daba igual; desde
que la pantalla de reinstalación lleva texto visible en español, un archivo sin
BOM imprimiría "ActualizaciÃ³n" en la ventana que ve el operador. El archivo
empieza por `EF BB BF` y cualquier editor que lo toque debe conservarlo.

### Desinstalación

El desinstalador (generado automáticamente por Inno Setup):
1. Mata el proceso del agente y sus hijos (`taskkill /F /T`)
2. Ejecuta `cronos-pos-agent.exe --disable-autostart` con `runasoriginaluser`, que
   elimina la clave del registro de la rama `HKCU` del operador real
3. Limpia `config.json`, logs y certificados de `%LOCALAPPDATA%\CronosAgent`, más
   los restos equivalentes junto al binario de instalaciones ≤ 1.3.0
4. Elimina el binario y las carpetas si quedan vacías

## Codificación de Acentos en Impresoras Térmicas (ESC/POS)

### El problema

Las ticketeras ESC/POS no entienden UTF-8: interpretan **cada byte por separado**
contra la página de códigos que tengan activa. Una `Á` en UTF-8 son dos bytes
(`0xC3 0x81`), así que la impresora saca dos símbolos ilegibles en su lugar.

Peor aún: la página de fábrica de la práctica totalidad de las ticketeras es
**CP437**, que de las cinco vocales acentuadas mayúsculas sólo contiene la `É`.
`Á`, `Í`, `Ó` y `Ú` no existen en esa tabla, así que no hay byte que enviar —
sólo cambiar de página de códigos resuelve el caso.

### La solución — dos mecanismos combinados

Implementados en `escpos.go` y aplicados dentro de `rawPrint()` en
`printer_windows.go` (y en `printer_darwin.go`), justo antes de escribir un solo
byte en el spooler.

**1. Selección de la página de códigos — `ESC t n`**

Se antepone al payload el comando que activa en el hardware una página con
soporte completo de español:

| Página | Comando | Bytes | Cobertura |
|---|---|---|---|
| **CP1252** (por defecto desde la v1.5.0) | `ESC t 16` | `1B 74 10` | Windows Latin-1: Á É Í Ó Ú Ñ ñ Ü ü ¿ ¡ € |
| CP850 | `ESC t 2` | `1B 74 02` | Multilingual Latin-1 (por defecto hasta la v1.4.0) |
| CP858 | `ESC t 19` | `1B 74 13` | CP850 + símbolo € |
| CP437 | `ESC t 0` | `1B 74 00` | USA/Standard Europe (página de fábrica) |
| `none` | — | — | Desactiva todo el tratamiento: los bytes viajan tal cual |

> **Ojo con `1B 74 13`.** En la tabla estándar de Epson —la que respetan
> prácticamente todos los clones— `n = 19` (`0x13`) es **PC858**, no CP1252.
> CP1252 es `n = 16` (`0x10`), que es lo que envía el agente. Ambas páginas
> imprimen bien los acentos del español, pero colocan los caracteres en bytes
> distintos, así que enviar `0x13` mientras se transcodifica a CP1252 imprimiría
> basura. Si un modelo concreto numera sus tablas de otra forma, se corrige con
> `escpos_code_page_id` (ver más abajo) sin recompilar.

El comando **no** se antepone ciegamente: si el payload empieza por `ESC @`
(`1B 40`, inicializar impresora), la selección se inyecta **después**. `ESC @`
reinicia la impresora y restaura la página de fábrica, así que anteponerla la
anularía y el ticket volvería a imprimir basura.

Si el payload ya trae su propio `ESC t` en la cabecera (primeros 64 bytes), se
asume que el frontend gestiona la codificación y el agente no interfiere.

**2. Transcodificación UTF-8 → bytes de la página de códigos (encoder Windows-1252)**

Éste es el mecanismo que de verdad arregla los acentos, y el motivo por el que
el prefijo `ESC t n` por sí solo no bastaba: **el texto llega al agente en
UTF-8**, donde una `Á` son *dos* bytes (`C3 81`). La ticketera no interpreta
UTF-8: lee byte a byte contra su tabla activa, así que imprime dos símbolos.
Hay que convertir la cadena a **un byte por carácter** antes de enviarla.

Desde la Fase 11 esa conversión la hace el codificador oficial de
`golang.org/x/text/encoding/charmap`, no una tabla mantenida a mano:

```go
encoder := charmap.Windows1252.NewEncoder()
encodedBytes, err := encoder.Bytes(textoDelTicket)   // "Á" -> 0xC1
```

`transcodeToCodePage()` convierte el texto a los bytes que la ticketera espera:

| Carácter | UTF-8 | CP850 | CP1252 |
|---|---|---|---|
| `Á` | `C3 81` | `B5` | `C1` |
| `É` | `C3 89` | `90` | `C9` |
| `Í` | `C3 8D` | `D6` | `CD` |
| `Ó` | `C3 93` | `E0` | `D3` |
| `Ú` | `C3 9A` | `E9` | `DA` |
| `Ñ` | `C3 91` | `A5` | `D1` |
| `Ü` | `C3 9C` | `9A` | `DC` |
| `¿` | `C2 BF` | `A8` | `BF` |

`escpos_codepages.go` ya no contiene tablas: son cuatro alias de `charmap`
(`Windows1252`, `CodePage850`, `CodePage858`, `CodePage437`). Se pasó de ~800
líneas generadas y revisadas a mano a la implementación de referencia del
proyecto Go — la misma que usan el resto de herramientas del ecosistema y que
nadie de este equipo tiene que auditar. El ASCII se copia sin traducir porque es
idéntico en las cuatro páginas, y ni siquiera entra en el codificador.

**Degradación cuando la página no representa un carácter.** El codificador
devuelve error si el texto contiene una runa que la página no tiene. Ese error
no puede tumbar un ticket, así que el camino rápido (una sola llamada a
`encoder.Bytes()` por tramo) tiene una red debajo: si falla, ese tramo se
recorre runa a runa con `charmap.EncodeRune()` y las que sobran se degradan a su
equivalente ASCII (`asciiFallback`: `Á`→`A`, `—`→`-`, `€`→`EUR`, `…`→`...`) y, en
último caso, a `?`. Nunca se imprime basura ni se pierde el ticket entero por un
carácter exótico.

**Por qué no se pasa el payload entero por el codificador.** Sería la forma
obvia de usar `encoder.Bytes()`, y rompería los tickets con logo: un payload RAW
**no es una cadena de texto**, sino texto mezclado con comandos y con datos
binarios. Ver "Por qué el recorrido es conservador" justo debajo — sólo se
codifican los tramos que de verdad son texto.

**El codificador se crea por impresión, no se comparte.** Es un transformador
con estado interno y `rawPrint()` puede ejecutarse en paralelo desde varias
peticiones HTTP; reutilizar uno global sería una condición de carrera.

### Por qué el recorrido es conservador

Un ticket RAW mezcla texto, comandos y datos binarios. Transcodificar a ciegas
corrompería un logo: en una imagen de 20 KB hay cientos de pares de bytes que
por casualidad forman UTF-8 válido, cada uno se colapsaría a un solo byte y, al
descuadrarse la longitud declarada, la impresora leería el resto del ticket como
comandos. Por eso el recorrido:

- Detecta los comandos gráficos por su cabecera y copia sus datos **en bloque**
  sin interpretarlos (`graphicsCommandLength`): `GS v 0` (raster, el habitual
  para logos), `ESC *`, `GS *`, la familia `GS ( fn pL pH` (incluye QR) y
  `GS 8 L` (gráficos con longitud de 32 bits). Si la longitud declarada excede
  el buffer se copia todo lo que queda: ante un payload truncado, copiar de más
  es más seguro que transcodificar.
- Copia sin tocar todo byte ASCII (`< 0x80`), que cubre el resto de comandos.
- Copia tal cual los bytes sueltos que no forman UTF-8 válido: son binarios.
- Agrupa las runas UTF-8 válidas consecutivas en **tramos de texto** y pasa cada
  tramo entero por `encoder.Bytes()`. Un comando ESC/POS nunca empieza por un
  byte ≥ `0x80`, así que un tramo no puede tragarse una cabecera.

### Por qué CP1252 es ahora la página por defecto — el caso «†nimo»

Síntoma en producción: el ticket imprimía **`†nimo`** donde debía decir
**`Ánimo`**. Un único carácter fuera de sitio, siempre en las mayúsculas
acentuadas, y sólo en algunas cajas.

La causa es que **la `Á` no tiene un byte universal**: cada página de códigos la
coloca en una posición distinta.

| Página activa en el hardware | Byte `0xB5` (la `Á` de CP850) se imprime como |
|---|---|
| CP850 | `Á` ✔ |
| CP1252 | `µ` |
| CP437 | `╡` |
| Otras tablas del firmware | cualquier otro símbolo — de ahí la `†` |

Con CP850 el agente enviaba `0xB5`, un byte que **sólo** significa `Á` en esa
página concreta. Basta con que la ticketera pierda la selección —un `ESC @` que
manda el frontend a mitad del ticket, un corte de corriente, un reinicio del
spooler, un modelo que arranca en su tabla de fábrica— para que ese mismo byte
se dibuje como el símbolo que ocupe esa posición en la tabla que quedó activa.

CP1252 elimina esa fragilidad en una caja de cobro Windows: sus bytes son los de
Latin-1 (`Á` = `0xC1`), que es lo que la ticketera y el propio sistema esperan
por defecto. Si la selección de página se pierde, el texto sigue saliendo bien.

Qué cambia exactamente:

1. `defaultCodePage` pasa de `cp850` a `cp1252` (`escpos.go`).
2. Todo flujo de impresión RAW abre con `1B 74 10` (`ESC t 16` → CP1252),
   inyectado detrás del `ESC @` inicial si el payload lo trae.
3. El texto se transcodifica a los bytes de CP1252 (`Á` → `0xC1`).
4. Los `config.json` ya existentes se migran de `cp850` a `cp1252` mediante
   `config_version` (ver "Migraciones de esquema"): sin esa migración las cajas
   ya instaladas habrían seguido imprimiendo mal después de actualizar.

### Ticketeras con numeración propia — `escpos_code_page_id`

El `n` de `ESC t n` sigue la tabla de Epson, pero algún clon numera sus páginas
de otra forma. En vez de recompilar, se fuerza el byte exacto:

```json
{ "escpos_code_page": "cp1252", "escpos_code_page_id": 19 }
```

El agente enviará `1B 74 13` pero seguirá transcodificando con la tabla de
CP1252. Es decir: **el comando y la tabla se configuran por separado**, porque
lo que hay que hacer coincidir es lo que la impresora entiende con lo que el
agente escribe. Fuera del rango 0–255 el valor se ignora con un aviso en el log.

El número describe cómo numera esa ticketera **una página concreta**, la
configurada. Si un ticket pide otra con `code_page`, el override se descarta y
se vuelve a la numeración estándar de Epson.

### Configuración

Global en `config.json` (`escpos_code_page`, `escpos_transcode`,
`escpos_code_page_id`) y anulable por ticket con los campos opcionales
`code_page` y `transcode` de `POST /api/print`. Se aceptan alias: `1252`,
`windows-1252`, `850`, `pc850`, `latin1`, `off`…

### Cobertura de tests

`escpos_test.go` — 17 casos: bytes exactos de las mayúsculas acentuadas en CP850
y CP1252, `Ánimo` completo con la configuración por defecto (`1B 74 10` + `C1`),
override del selector, degradación a ASCII dentro de un tramo de texto y en
CP437, integridad de los comandos ESC/POS, logo raster con UTF-8 incrustado que
debe salir intacto, inserción después de `ESC @`, respeto a un `ESC t` propio del
frontend y resolución de alias.

Los tests comprueban **bytes exactos**, así que también sirvieron de red al
sustituir las tablas propias por `charmap`: si el codificador de `x/text`
colocara un solo carácter en otra posición, la suite fallaría.

```bash
go test ./...   # ejecutar desde Windows o macOS (el paquete no compila en Linux)
```

## Identidad Visual — Icono del Gato Tuxedo

### Qué se embebe y por qué

El agente es un binario `-H=windowsgui`: no abre ventana, no aparece en la barra
de tareas al arrancar y su única presencia visible es un icono de 16 px en el
System Tray. Hasta la v1.4.0 ese icono era el genérico de Windows, y el operador
no sabía distinguirlo del resto de la bandeja.

Los recursos gráficos van **dentro** del ejecutable con `//go:embed`:

```go
//go:embed app_icon.ico
var appIconICO []byte

//go:embed welcome_cat.png
var welcomeCatPNG []byte
```

No es una preferencia de estilo: el binario **se sobrescribe entero** en cada
actualización y se copia solo a `C:\Program Files\CronosAgent` desde donde lo
haya dejado el operador (ver "Ubicación Permanente del Binario"). Un icono que
viviera como archivo suelto junto al `.exe` se perdería en la primera
actualización o en la primera reubicación, y el agente volvería al icono
genérico. Embebido, el ejecutable es autónomo.

`main.go` lo aplica al arrancar la bandeja, antes del tooltip:

```go
systray.SetIcon(trayIcon())
```

### Icono dinámico de estado: gris → verde

El icono no sólo identifica al agente, **informa de si está sano**. Son tres
dibujos del mismo gato generados con paletas distintas:

| Estado | Icono | Cuándo | Tooltip / menú |
|---|---|---|---|
| Iniciando | `app_icon_gray.ico` — gato **gris**, ojos apagados | Desde `onReady()`: el proceso vive, pero todavía carga configuración y resuelve el puerto | "Iniciando…" |
| Operativo | `app_icon_green.ico` — gato normal con **punto verde** | En cuanto el socket acepta conexiones | "Operativo (:9100)" |
| Detenido | vuelve al **gris** | Si el servidor muere por su cuenta | "Detenido" |

```go
systray.SetIcon(trayIconStarting())      // gris: aún no escucha
...
listener, err := net.Listen("tcp", addr) // el puerto se abre AQUÍ
...
systray.SetIcon(trayIconReady())         // verde: ya acepta conexiones
```

**El puerto se abre con `net.Listen` explícito, no dentro de
`ListenAndServe()`.** Es la diferencia entre un semáforo honesto y uno
decorativo: `ListenAndServe` se llama desde una goroutine y devuelve el error
*después*, así que poner el icono verde antes significaría "hemos lanzado una
goroutine que quizá lo consiga". Con el listener creado antes, el verde sólo
aparece cuando el socket está realmente aceptando conexiones; si el bind falla
—porque otra instancia ganó la carrera por el puerto— el icono se queda gris y
el motivo queda en el log. `srv.Serve(listener)` recibe ese listener ya abierto.

El punto verde vive en la esquina inferior derecha con un halo blanco. A 16 px
—el tamaño real en la bandeja de Windows— el color del pelaje no se distingue,
pero 4 píxeles de verde saturado sí: por eso el estado se marca con un punto y
no tiñendo el gato entero. El gris, en cambio, desatura todo el dibujo (pelaje,
ojos y nariz), que es lo que lo hace leerse como "apagado" de un vistazo.

### Un icono por plataforma

`trayIcon()` está definido dos veces con build tags porque cada System Tray
exige un formato distinto:

| Plataforma | Archivos | Formato | Motivo |
|---|---|---|---|
| Windows | `assets_windows.go` → `app_icon{,_gray,_green}.ico` | `.ico` (16, 24, 32, 48, 64, 128, 256 px) | `Shell_NotifyIcon` carga el icono con `LoadImage`, que sólo lee `.ico` |
| macOS | `assets_darwin.go` → `app_icon_{gray,green}.png` | PNG 64×64 | `NSStatusItem` se dibuja con `NSImage`, que reescala a 16 pt |

`trayIconStarting()` y `trayIconReady()` están definidas en los dos archivos con
build tags, así que `main.go` cambia de estado sin saber en qué plataforma corre.

Las resoluciones pequeñas del `.ico` se dibujan por separado, no reduciendo la
grande: a 16×16 cualquier detalle se convierte en ruido, así que el icono es
deliberadamente plano — silueta negra, mancha blanca del hocico y ojos dorados.

### Icono del ejecutable y manifiesto (`rsrc_windows_amd64.syso`)

`//go:embed` resuelve el icono de la bandeja, pero **no** el que muestran el
Explorador, Alt+Tab o el panel de aplicaciones instaladas: ése tiene que ser un
recurso Win32 del PE. Se genera con `rsrc` y el enlazador de Go lo incorpora
solo por el sufijo `_windows_amd64` del nombre:

```bash
go run github.com/akavel/rsrc@v0.10.2 \
  -ico app_icon.ico -manifest app.manifest -arch amd64 -o rsrc_windows_amd64.syso
```

El mismo `.syso` embebe `app.manifest`, que aporta dos cosas:

- `requestedExecutionLevel asInvoker` — el agente nunca pide elevación: `HKCU` y
  `%LOCALAPPDATA%` deben ser los del operador de la caja, no los de un
  administrador.
- Dependencia de **Common Controls 6.0** — sin ella el botón "Cerrar" de la
  ventana de bienvenida se dibujaría con el estilo gris de Windows 95.

No se declara compatibilidad con DPI a propósito: la ventana de bienvenida usa
medidas fijas en píxeles, así que en una pantalla al 150 % es preferible que
Windows escale la ventana entera a que la dibuje a dos tercios de su tamaño.

### Los recursos se dibujan por código

`tools/genassets` genera los tres archivos dibujando figuras (elipses,
polígonos, Béziers) sobre un lienzo supermuestreado ×4 que después se reduce
promediando bloques, que es de donde sale el suavizado de bordes:

```bash
go run ./tools/genassets     # reescribe app_icon.ico, app_icon.png y welcome_cat.png
```

Así el repositorio no arrastra binarios opacos de origen desconocido: el gato
es reproducible, auditable y modificable en un `diff`. El `.ico` se serializa a
mano (ICONDIR + ICONDIRENTRY + DIB de 32 bits, con las dos resoluciones grandes
comprimidas en PNG para no engordar el archivo 260 KB).

## Cierre Limpio del Agente

Salir del agente no es sólo terminar el proceso: hay un socket escuchando en
`127.0.0.1:9100` y puede haber un ticket viajando hacia el spooler.

### Un único punto de salida

Las tres formas de cerrar el agente —**"Salir"** en el menú del System Tray,
`SIGINT`/`SIGTERM`, y el cierre de sesión de Windows— convergen en
`systray.Quit()`, que hace terminar el bucle de la bandeja y llama a `onExit()`.
Ahí se libera todo:

```go
func onExit() {
    exitOnce.Do(func() {
        close(agentDone)      // detiene el polling de updates y el bucle del menú
        shutdownHTTPServer()  // cierra el servidor y libera el puerto
        log.Println("Cronos Agent finalizado.")
    })
}
```

| Recurso | Cómo se libera |
|---|---|
| Socket de escucha | `srv.Shutdown(ctx)`, que cierra el listener y, con él, el puerto |
| Peticiones en curso | `Shutdown` espera a que terminen, con un tope de 5 s (`shutdownTimeout`); pasado ese plazo, `srv.Close()` |
| Goroutine del updater | Termina por el `select` sobre `agentDone` (antes era un `for range ticker.C` infinito) |
| Goroutine del menú | Ídem: un `case <-agentDone: return` la saca del bucle |
| Archivo de log | El `defer logCloser.Close()` de `main()`, que corre después de `systray.Run` |

### Detalles que importan

- **`Shutdown` y no `Close`**: cerrar a lo bruto cortaría una petición
  `POST /api/print` a medio escribir en el spooler, y la ticketera imprimiría
  medio ticket. `Shutdown` deja terminar lo que ya estaba en marcha.
- **Liberar el puerto es funcional, no cosmético**: si el socket queda ocupado,
  el siguiente arranque cae al 9101 por el fallback de `ResolvePort` y el
  frontend, que sigue apuntando al 9100, deja de encontrar el agente.
- **`sync.Once`**: cerrar dos veces un canal entra en pánico, y la salida puede
  llegar por varias vías a la vez. `exitOnce` hace `onExit()` idempotente sin
  depender de que systray las serialice.
- **`atomic.Pointer[http.Server]`**: quien guarda el servidor (`onReady`) y quien
  lo cierra (`onExit`) son goroutines distintas. El `Swap(nil)` además garantiza
  que sólo se cierre una vez.

## Ventana de Bienvenida Post-Instalación (`--first-run`)

### El problema

Una instalación atendida terminaba sin ninguna señal: el instalador cierra su
barra de progreso, el agente arranca oculto y lo único que cambia en pantalla es
un icono de 16 px en la bandeja. El operador de la caja no tenía forma de saber
si había funcionado, y la duda acababa en una llamada a soporte.

### El flujo

1. El instalador lanza el agente con `--first-run` al terminar la barra de
   progreso (`[Run]` de `setup.iss`).
2. El agente arranca con normalidad —self-healing, reubicación, auto-arranque,
   bandeja y servidor HTTP— y **además** abre la ventana de bienvenida.
3. El operador la cierra con el botón "Cerrar" (o con la X de la barra de
   título) y el agente sigue funcionando en la bandeja.

### Por qué la ventana la abre el propio agente

Podría lanzarse un segundo proceso sólo para la ventana, pero
`killOrphanInstances()` mata al arrancar cualquier otra instancia de
`cronos-pos-agent.exe` que no sea la actual (ver "Self-Healing"). Ese segundo
proceso moriría en cuanto el agente hiciera su limpieza —o al revés, según quién
ganara la carrera—. Abriéndola desde el mismo proceso no hay carrera posible.

La ventana corre en su propia goroutine con `runtime.LockOSThread()`, porque el
bucle de mensajes de Win32 es por hilo y `systray.Run()` se queda con el
principal hasta que el usuario cierra el agente.

Si el binario tiene que reubicarse a la ruta permanente, el flag viaja con el
relanzado (`EnsurePermanentLocation("--first-run")`): la ventana debe abrirla la
instancia definitiva, no la temporal que está a punto de terminar.

### Contenido de la ventana

| Elemento | Detalle |
|---|---|
| Ilustración | `welcome_cat.png` (880×440, embebida): el gato tuxedo jugando con la ticketera y su ticket |
| Mensaje | **"¡Cronos POS se ha instalado correctamente!"** |
| Texto secundario | Explica que el agente ya está en la bandeja y que arrancará solo con el equipo |
| Botón | "✕ Cerrar", con el aspa clásica; la ventana conserva además la X de la barra de título |
| Icono de la ventana | El gato tuxedo (`app_icon.ico`, cargado con `LoadImageW`) |
| Tamaño | Área cliente de 468×384 px, centrada en la pantalla |

### Por qué Win32 nativo y no una librería

`firstrun_windows.go` habla directamente con `user32.dll` y `gdi32.dll` vía
`syscall`, igual que ya hacía `printer_windows.go` con `ShellExecuteW`:

| Alternativa | Por qué se descartó |
|---|---|
| `github.com/lxn/win` | Sin mantenimiento desde 2021. Obliga a escribir exactamente el mismo código (registrar la clase, bucle de mensajes, pintar el bitmap) con otra fachada |
| `fyne` | Requiere CGO y OpenGL, y añade decenas de MB al binario para una ventana que se abre una vez en la vida del equipo |
| `MessageBox` nativa | No admite imagen propia, y el requisito es mostrar la ilustración |

Resultado: la ventana no añadió **ninguna dependencia** a `go.mod` y el
ejecutable sigue siendo un único archivo autónomo.

Detalle de implementación: `StretchDIBits` ignora el canal alfa de un DIB
`BI_RGB`, así que la transparencia del PNG se compone contra el blanco de la
ventana al decodificarlo; de lo contrario los bordes suavizados de la
ilustración saldrían con un halo gris. La imagen se guarda al doble del tamaño
con el que se dibuja y se reduce con `HALFTONE`, para que se vea nítida cuando
Windows escala la ventana en pantallas HiDPI.

### Una sola vez

El agente escribe un marcador `welcome-shown` en su directorio de datos
(`%LOCALAPPDATA%\CronosAgent\`) con la versión que ya se mostró:

- Relanzar el binario con `--first-run` no repite la ventana.
- Actualizar a una versión nueva sí vuelve a confirmarle al operador que la
  actualización ha ido bien.
- El marcador se escribe **antes** de abrir la ventana: si el subsistema
  gráfico fallara, el agente no debe quedarse intentándolo en cada arranque.
- El desinstalador lo borra, para que una reinstalación vuelva a saludar.

En despliegues silenciosos (`/VERYSILENT`) la ventana **no** se abre: el
instalador usa `Check: not WizardSilent` y lanza el agente sin el flag. No hay
nadie delante de esas pantallas que la cierre.

En macOS `showWelcomeWindow()` muestra el diálogo equivalente con `osascript`.
No se replica la ventana con ilustración porque el problema que la motiva es de
Windows: en macOS la app se instala arrastrándola a `/Applications` y el propio
Finder da esa confirmación.

## Seguridad — Token Local

**Flujo de autenticación:**
1. El frontend React lee el token de `config.json` (o lo recibe del instalador/setup).
2. Toda petición a `/api/*` debe incluir el header `X-Cronos-Agent-Token: <token>`.
3. Si el header falta o no coincide, el agente responde `401 Unauthorized`.
4. El endpoint `/health` está exento de autenticación.

## Endpoints HTTP

Base: `http://127.0.0.1:{port}` (puerto dinámico, default 9100)

| Método | Ruta | Auth | Descripción |
|---|---|---|---|
| `GET` | `/health` | No | Health check básico (status, service, version) |
| `GET` | `/api/health` | Si | Diagnóstico con uptime y uso de RAM |
| `GET` | `/api/printers` | Si | Lista impresoras instaladas en el SO |
| `GET` | `/api/printers/queue` | Si | Cola de impresión de una impresora específica |
| `POST` | `/api/print` | Si | Envía datos RAW (ESC/POS) a una impresora térmica |
| `POST` | `/api/print/pdf` | Si | Imprime un archivo PDF en una impresora convencional |

### `POST /api/print` — Cuerpo de la petición

```json
{
  "printer_name": "POS-80",
  "printer_data": "G0BBUlTDjUNVTE8gw5FPw5FPCh1WAA==",
  "code_page": "cp1252",
  "transcode": true
}
```

El `printer_data` del ejemplo es `ESC @` + `"ARTÍCULO ÑOÑO\n"` en UTF-8 + corte
(`GS V 0`). Así lo transforma el agente antes de mandarlo al spooler:

```
Recibido:  1B 40             41 52 54 C3 8D 43 55 4C 4F 20 C3 91 4F C3 91 4F 0A  1D 56 00
Enviado:   1B 40  1B 74 10   41 52 54 CD    43 55 4C 4F 20 D1    4F D1    4F 0A  1D 56 00
                  ^^^^^^^^            ^^                ^^       ^^
                  ESC t 16 tras ESC @ Í en CP1252       Ñ en CP1252
```

| Campo | Tipo | Obligatorio | Descripción |
|---|---|---|---|
| `printer_name` | `string` | Sí | Nombre de la impresora en el SO |
| `printer_data` | `string` | Sí | Ticket ESC/POS en Base64. El texto va en UTF-8: el agente lo transcodifica |
| `code_page` | `string` | No | Anula `escpos_code_page` sólo para este ticket. `400` si no está soportada |
| `transcode` | `bool` | No | Anula `escpos_transcode` sólo para este ticket |

## Dependencias Externas

| Módulo | Versión | Uso |
|---|---|---|
| `github.com/getlantern/systray` | v1.2.2 | Icono y menú en barra de tareas |
| `github.com/alexbrainman/printer` | v0.0.0-20200912 | Windows Print Spooler |
| `github.com/atotto/clipboard` | v0.1.4 | Copiar el token de seguridad al portapapeles del SO (multiplataforma) |
| `golang.org/x/sys` | v0.1.0+ | Registro de Windows |
| `golang.org/x/text` | v0.23.0 | `encoding/charmap`: codificación UTF-8 → CP1252/CP850/CP858/CP437 |

`golang.org/x/text` es la única dependencia añadida en la Fase 11, y **resta
código en vez de sumarlo**: sustituye las ~800 líneas de tablas de códigos que
se mantenían en este repositorio. Se fija en la v0.23.0 porque las versiones
posteriores exigen Go ≥ 1.25 y el proyecto compila con la 1.24.

Todo lo demás sigue siendo stdlib: el icono se embebe con `//go:embed`, las
ilustraciones se generan con `image`/`image/png` y la ventana de bienvenida
habla directamente con `user32`/`gdi32` vía `syscall`.

`github.com/akavel/rsrc` se usa como herramienta puntual (`go run …@v0.10.2`)
para regenerar `rsrc_windows_amd64.syso`; no aparece en `go.mod` ni se enlaza.

## Compilación para Producción

### Windows x64 (desde macOS M4 Pro):

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
  go build -ldflags="-H=windowsgui -w -s" -o build/cronos-pos-agent.exe .
```

### Mac ARM nativo:

```bash
GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 \
  go build -ldflags="-w -s" -o cronos-pos-agent .
```

Los recursos gráficos ya están versionados en el repositorio, así que compilar
**no** requiere regenerarlos. Sólo hace falta si se cambia el dibujo del gato:

```bash
go run ./tools/genassets                       # los 3 .ico + los 3 .png + welcome_cat.png
go run github.com/akavel/rsrc@v0.10.2 \        # rsrc_windows_amd64.syso (icono + manifiesto)
  -ico app_icon.ico -manifest app.manifest -arch amd64 -o rsrc_windows_amd64.syso
```

El `.syso` embebe la versión declarada en `app.manifest`, así que hay que
regenerarlo al subir de versión.

### Pipeline completo de distribución Windows:

```bash
# 1. Compilar binario optimizado (enlaza rsrc_windows_amd64.syso automáticamente
#    por el sufijo del nombre, y embebe el icono y la ilustración con go:embed)
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
  go build -ldflags="-H=windowsgui -w -s" -o build/cronos-pos-agent.exe .

# 2. Generar instalador (ejecutar en Windows)
ISCC.exe installer/setup.iss

# 3. Resultado: installer/Output/CronosAgentSetup-1.6.0.exe
# 4. Despliegue silencioso en cajas de cobro:
#    CronosAgentSetup-1.6.0.exe /VERYSILENT /SUPPRESSMSGBOXES /NORESTART
```

## Fases — Historial Completo

### Fase 1: Inicialización ✓
### Fase 2: Autodescubrimiento ✓
### Fase 3: Motor RAW ESC/POS ✓
### Fase 4: Seguridad, Autostart, Build ✓
### Fase 5: CORS Dinámico y Monitoreo de Spooler ✓
### Fase 6: Suite Enterprise e Instalador Windows ✓
- ~~Conmutación dinámica de puertos (fallback 9100→9110)~~ ✓
- ~~Self-healing: detección y eliminación de instancias huérfanas~~ ✓
- ~~Generación nativa de certificados SSL (`--generate-certs`)~~ ✓
- ~~Instalador silencioso Windows (Inno Setup)~~ ✓
- ~~Campo `port` en config.json~~ ✓

### Fase 7: Conversión Gráfica y Soporte PDF ✓
- ~~Endpoint `POST /api/print/pdf` para impresión de documentos PDF~~ ✓
- ~~Impresión nativa de PDF en macOS via CUPS (`lp -d`)~~ ✓
- ~~Impresión silenciosa de PDF en Windows via `ShellExecuteW`~~ ✓
- ~~Tipo `PDFPrintRequest` en `printer.go`~~ ✓
- ~~Versión del agente actualizada a 1.3.0~~ ✓

### Fase 8: Silenciamiento de Consolas, Token al Portapapeles y Auto-arranque Robusto ✓
- ~~Inyección de `CREATE_NO_WINDOW` en todos los subprocesos de Windows (anti-parpadeo)~~ ✓
- ~~Opción "Copiar Token de Seguridad" en el Systray~~ ✓
- ~~Ruta del ejecutable entre comillas dobles en el registro de autostart (soporte de espacios)~~ ✓

### Fase 9: Persistencia Permanente y Codificación de Acentos ✓
- ~~Ruta fija y obligatoria del binario en `C:\Program Files\CronosAgent\` (fallback `C:\ProgramData\CronosAgent\`)~~ ✓
- ~~Auto-reubicación del binario lanzado desde carpetas volátiles, con migración de `config.json` y certificados~~ ✓
- ~~Registro de autostart apuntando siempre a la ruta permanente y entre comillas dobles~~ ✓
- ~~Reparación automática de la entrada del registro en cada arranque (`EnsureAutostartRegistered`)~~ ✓
- ~~Separación del directorio de datos (`%LOCALAPPDATA%\CronosAgent`) respecto del binario~~ ✓
- ~~Selección de página de códigos ESC/POS (`ESC t n`) y transcodificación UTF-8 → CP850/CP858/CP1252/CP437~~ ✓
- ~~Detección de comandos gráficos para no corromper logos raster al transcodificar~~ ✓
- ~~Overrides `code_page` / `transcode` en `POST /api/print` y en `config.json`~~ ✓
- ~~Instalador con `PrivilegesRequired=admin` + `runasoriginaluser` y desinstalación de la clave HKCU correcta~~ ✓
- ~~Suite de tests `escpos_test.go` y versión del agente actualizada a 1.4.0~~ ✓

### Fase 10: Pulido Final — Encoding, Icono y Bienvenida ✓
- ~~Página de códigos por defecto CP1252 (`ESC t 16` = `1B 74 10`) en todo flujo de impresión RAW~~ ✓
- ~~Migración automática del `config.json` existente de `cp850` a `cp1252` vía `config_version`~~ ✓
- ~~Override `escpos_code_page_id` para ticketeras con numeración de tablas propia~~ ✓
- ~~Icono del gato tuxedo embebido con `//go:embed app_icon.ico` y aplicado con `systray.SetIcon`~~ ✓
- ~~Icono PNG equivalente para la barra de menús de macOS~~ ✓
- ~~Recurso Win32 (`rsrc_windows_amd64.syso`): icono del ejecutable + manifiesto con Common Controls 6~~ ✓
- ~~Generador reproducible de los recursos gráficos (`tools/genassets`)~~ ✓
- ~~Ventana de bienvenida post-instalación nativa Win32 con ilustración, mensaje y botón "Cerrar"~~ ✓
- ~~Flag `--first-run`, marcador `welcome-shown` por versión y propagación al relanzado~~ ✓
- ~~Instalador: icono propio, lanzamiento con `--first-run` y omisión de la ventana en `/VERYSILENT`~~ ✓
- ~~Dos tests nuevos (`Ánimo` con la configuración por defecto y override del selector) y versión 1.5.0~~ ✓

### Fase 11: Estabilización de Iconos Dinámicos y Transcodificación de Acentos ✓
- ~~Icono dinámico en el System Tray: gris (iniciando) → verde (operativo), y vuelta a gris si el servidor cae~~ ✓
- ~~El verde se activa tras un `net.Listen` explícito: significa "el socket acepta conexiones", no "se ha lanzado una goroutine"~~ ✓
- ~~Tooltip y entrada de estado del menú sincronizados con el icono (`SetTooltip` / `mStatus.SetTitle`)~~ ✓
- ~~Tercera y segunda variantes del gato generadas por `tools/genassets` con paletas propias y punto de estado~~ ✓
- ~~Transcodificación con el codificador oficial `charmap.Windows1252.NewEncoder()` de `golang.org/x/text`~~ ✓
- ~~Eliminadas ~800 líneas de tablas de páginas de códigos mantenidas a mano (`escpos_codepages.go`)~~ ✓
- ~~Codificación por tramos de texto, con degradación runa a runa cuando la página no representa un carácter~~ ✓
- ~~Codificador creado por impresión: `rawPrint()` puede ejecutarse en paralelo desde varias peticiones~~ ✓
- ~~Cierre limpio en `onExit()`: `srv.Shutdown(ctx)` con 5 s de gracia, puerto liberado y goroutines detenidas~~ ✓
- ~~Canal `agentDone` + `sync.Once` para que "Salir", SIGINT y SIGTERM converjan en una salida idempotente~~ ✓
- ~~Test nuevo de degradación dentro de un tramo de texto (17 casos) y versión 1.6.0~~ ✓

### Fase 12: Instalador con Manejo Inteligente de Actualizaciones ✓
- ~~Detección de instalación previa en `InitializeSetup()` leyendo `Uninstall\{AppId}_is1` en HKLM, HKCU y sus vistas de 32 bits~~ ✓
- ~~Pantalla propia de "Actualización / Reinstalación detectada" con `CreateCustomForm`, lista de lo que se conserva y datos de la versión encontrada~~ ✓
- ~~La pantalla se omite en `/SILENT` y `/VERYSILENT` (`WizardSilent`), donde no hay nadie que pueda cerrarla~~ ✓
- ~~Reutilización de la ruta de la instalación anterior como destino (`PermanentInstallDir` + `UsePreviousAppDir`), para no dejar dos copias del agente~~ ✓
- ~~`--generate-certs` sólo en instalación nueva (`Check: not IsUpgradeInstall`): una actualización ya no reemite el par RSA~~ ✓
- ~~Cierre preventivo del proceso en `PrepareToInstall()`: `taskkill /T` ordenado y `/F /T` como último recurso, con sondeo de `tasklist` hasta confirmar que el binario está libre~~ ✓
- ~~Aborto con mensaje accionable si el agente sobrevive, en vez de fallar a mitad de la copia con "archivo en uso"~~ ✓
- ~~`SetupLogging=yes` y copia del registro a `{app}\install-log.txt` en `ssDone`, con trazas propias del script marcadas `[Cronos]`~~ ✓

## Ocultación Total de Consola en Windows — `CREATE_NO_WINDOW`

Los subprocesos nativos de Windows (`powershell` para `Get-PrintJob`, `tasklist` para self-healing) provocaban un parpadeo de ventana de consola/PowerShell cada vez que se consultaba la lista o la cola de impresoras. Para eliminarlo por completo se inyecta la bandera nativa `CREATE_NO_WINDOW` (`0x08000000`) junto con `HideWindow` en el `SysProcAttr` de **cada** invocación.

En `printer_windows.go` se centraliza esto en un helper que sustituye a `exec.Command` en todas las llamadas de la plataforma:

```go
const createNoWindow = 0x08000000 // CREATE_NO_WINDOW

func hiddenCommand(name string, args ...string) *exec.Cmd {
    cmd := exec.Command(name, args...)
    cmd.SysProcAttr = &syscall.SysProcAttr{
        HideWindow:    true,
        CreationFlags: createNoWindow,
    }
    return cmd
}
```

Todo subproceso o comando nativo invocado por el agente (`powershell`, `tasklist`) pasa por `hiddenCommand`, garantizando que ninguna ventana de consola se levante en pantalla. macOS no requiere este tratamiento porque `lp`/`lpstat` no abren ventanas.

## Copiar Token de Seguridad al Portapapeles (Systray)

Se agregó el ítem de menú `mCopyToken` en `main.go`:

```go
mCopyToken := systray.AddMenuItem("Copiar Token de Seguridad", "Copia el token del agente al portapapeles")
```

Al hacer clic, la función `copyTokenToClipboard()`:

1. Lee el `api_token` guardado en `config.json` (vía `LoadConfig()`).
2. Copia la cadena al portapapeles del SO con la librería multiplataforma **`github.com/atotto/clipboard`** (`clipboard.WriteAll`) — usa `pbcopy` en macOS y la API nativa del portapapeles en Windows, sin comandos externos que abran ventanas.
3. Registra en el log la confirmación discreta: `"Token copiado al portapapeles"`.

Esto evita que el operador tenga que abrir manualmente `config.json` para copiar el token que el frontend necesita en el header `X-Cronos-Agent-Token`.

## Arranque Silencioso Garantizado (Windows)

El flag de compilación `-H=windowsgui` (el propio binario no abre consola) y
`CREATE_NO_WINDOW` en todos los subprocesos hacen que el agente arranque de forma
totalmente transparente y se aloje de inmediato en el System Tray sin mostrar
ninguna ventana.

La ruta que se registra en `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
y su reparación automática se documentan en **"Auto-arranque a Prueba de
Reinicios (Windows)"**. Desde la v1.4.0 ya no se registra la ruta de
`os.Executable()` sino la ubicación permanente del binario: registrar la ruta del
proceso en curso era lo que rompía el arranque cuando el agente se había lanzado
desde una carpeta temporal.

## Endpoint `POST /api/print/pdf` — Detalle Técnico

### Request

```
POST /api/print/pdf
Header: X-Cronos-Agent-Token: <token>
Content-Type: application/json
```

```json
{
  "printer_name": "Nombre_Impresora_Oficina",
  "pdf_data": "JVBERi0xLjQKMS... (Base64 del archivo PDF)"
}
```

### Respuesta exitosa (200)

```json
{
  "status": "ok",
  "message": "PDF enviado a la impresora correctamente"
}
```

### Errores

| Código | Causa |
|---|---|
| 400 | JSON inválido, campos vacíos, o Base64 malformado |
| 401 | Token de autenticación ausente o inválido |
| 405 | Método HTTP distinto a POST |
| 500 | Error creando archivo temporal, o el SO rechazó la orden de impresión |

### Flujo interno

1. El handler decodifica el Base64 de `pdf_data` a bytes
2. Crea un archivo temporal seguro (`os.CreateTemp`) con extensión `.pdf`
3. Escribe los bytes al archivo temporal
4. Invoca la función `printPDF()` específica de la plataforma (build tags)
5. El archivo temporal se elimina con `defer os.Remove()` tras enviar a la cola

### Implementación por plataforma

#### macOS (CUPS)

CUPS maneja PDF de forma nativa sin conversión. Se ejecuta:

```
lp -d "Nombre_Impresora" /ruta/archivo_temporal.pdf
```

No se usa la flag `-o raw` (a diferencia del endpoint ESC/POS) porque CUPS debe procesar el PDF a través de sus filtros de renderizado para enviarlo como datos rasterizados a la impresora.

#### Windows (ShellExecuteW)

Las impresoras estándar de Windows no aceptan datos PDF crudos a través del Spooler (a diferencia de las impresoras térmicas con ESC/POS RAW). El PDF debe pasar por el subsistema de impresión del sistema operativo.

**Mecanismo:** Se utiliza la API nativa `ShellExecuteW` de `shell32.dll` a través del paquete `syscall` de Go:

```go
shell32 := syscall.NewLazyDLL("shell32.dll")
shellExecute := shell32.NewProc("ShellExecuteW")
ret, _, _ := shellExecute.Call(
    0,                                    // hwnd: sin ventana padre
    uintptr(unsafe.Pointer(verbPtr)),     // lpOperation: "print"
    uintptr(unsafe.Pointer(filePtr)),     // lpFile: ruta al PDF temporal
    uintptr(unsafe.Pointer(paramsPtr)),   // lpParameters: /p /h "impresora"
    0,                                    // lpDirectory: nil
    0,                                    // nShowCmd: SW_HIDE (oculto)
)
```

**¿Por qué `ShellExecuteW`?**

- El verbo `"print"` delega la impresión al programa asociado a archivos `.pdf` en el registro de Windows (Adobe Acrobat, Foxit Reader, SumatraPDF, Microsoft Edge, etc.)
- `SW_HIDE` (valor `0`) asegura que no se levante ninguna interfaz gráfica visible
- El valor de retorno `> 32` indica éxito; valores `<= 32` son códigos de error de Windows
- No requiere CGO ni dependencias externas adicionales — usa `syscall` y `unsafe` de la stdlib
- Compatible con cualquier lector PDF instalado en el sistema, ya que Windows mantiene la asociación de archivos en `HKEY_CLASSES_ROOT\.pdf`

### Pendiente (fuera de scope actual)
- Comunicación bidireccional (WebSocket/SSE)
- Descarga automática de binarios en auto-update
- Firma de binarios (code signing)
- HTTPS nativo usando los certificados generados
