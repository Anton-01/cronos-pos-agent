# Cronos POS Agent — Contexto del Proyecto

## Estado Actual

**Fase 9: Persistencia Permanente estilo QZ Tray y Codificación de Acentos ESC/POS** — Completado

Fases completadas: 1 (Inicialización), 2 (Autodescubrimiento), 3 (Motor RAW ESC/POS), 4 (Seguridad, Autostart, Build), 5 (CORS dinámico, Health, Monitoreo de cola), 6 (Port fallback, Self-healing, Certificados SSL nativos, Instalador Inno Setup), 7 (Impresión nativa de PDF en impresoras convencionales), 8 (CREATE_NO_WINDOW anti-parpadeo, copiar token al portapapeles, autostart con ruta entre comillas), 9 (Ruta permanente en Program Files, auto-reubicación y reparación del registro, páginas de códigos ESC/POS con transcodificación de acentos).

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
| Tablas de códigos | Generadas de los codecs cp850/cp858/cp1252/cp437 | Cero dependencias externas, valores verificados contra los codecs estándar |
| Logs | Rotación nativa con `RotatingLogger` | Sin dependencias externas, 10MB max, 3 backups |
| Updates | Goroutine con polling cada 6h | Consulta JSON remoto |
| Instalador | Inno Setup 6.3+ | Instalador silencioso estándar de Windows; pide admin para Program Files y cae a ProgramData si no lo hay |

### Estructura de Archivos

```
cronos-pos-agent/
├── main.go              # Entry point: flags CLI, self-healing, reubicación, systray, goroutines
├── server.go            # Router, middlewares (CORS dinámico + Auth), handlers (6 endpoints)
├── config.go            # Carga/generación de config.json, AgentVersion (1.4.0), preferencias
├── network.go           # ResolvePort: fallback dinámico de puertos con scan
├── certs.go             # GenerateCerts: RSA 2048 + X.509 autofirmado nativo
├── logger.go            # RotatingLogger: escritura a archivo con rotación 10MB/3 backups
├── updater.go           # CheckForUpdates: polling de versión contra servidor central
├── printer.go           # Tipos compartidos (PrinterInfo, PrintRequest, QueueInfo, PrintJob)
├── escpos.go            # Motor de codificación: ESC t n, transcodificación y salto de gráficos
├── escpos_codepages.go  # Tablas generadas CP850 / CP858 / CP1252 / CP437 + fallback ASCII
├── escpos_test.go       # Tests del motor de codificación (14 casos)
├── paths_windows.go     # Build tag: windows — ruta permanente, reubicación, directorio de datos
├── paths_darwin.go      # Build tag: darwin — directorio de datos y reparación del LaunchAgent
├── printer_windows.go   # Build tag: windows — spooler, RAW, cola, autostart, killOrphan
├── printer_darwin.go    # Build tag: darwin — CUPS, RAW, cola, autostart, killOrphan
├── installer/
│   └── setup.iss        # Script Inno Setup para instalador silencioso Windows
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

## Archivo `config.json` — Esquema Completo

```json
{
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
  "escpos_code_page": "cp850",
  "escpos_transcode": true,
  "autostart": true
}
```

| Propiedad | Tipo | Default | Descripción |
|---|---|---|---|
| `api_token` | `string` | UUID v4 auto | Token de autenticación para header `X-Cronos-Agent-Token` |
| `allowed_origins` | `string[]` | 5 orígenes | Lista de orígenes CORS permitidos |
| `update_url` | `string` | pos-app.tech | URL del JSON de versión para auto-updates |
| `port` | `int` | `9100` | Puerto preferido. Si está ocupado, busca el siguiente libre (9101–9110) |
| `escpos_code_page` | `string` | `"cp850"` | Página de códigos que se activa en la ticketera: `cp850`, `cp858`, `cp1252`, `cp437` o `none` |
| `escpos_transcode` | `bool` | `true` | Convierte el texto UTF-8 a los bytes de esa página de códigos |
| `autostart` | `bool` | `true` | Preferencia de arranque con el sistema. El agente sólo repara la entrada del registro si es `true` |

Las claves nuevas se añaden automáticamente al `config.json` existente en el
primer arranque de la v1.4.0, sin perder el `api_token` ya emitido al frontend.

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
| 1 | Cierra instancias previas | `taskkill /F /IM cronos-pos-agent.exe` via `PrepareToInstall()` |
| 2 | Copia binario a la ruta permanente | `C:\Program Files\CronosAgent\` con admin, `C:\ProgramData\CronosAgent\` sin elevación (`PermanentInstallDir`) |
| 3 | Genera certificados SSL | `--generate-certs` en modo oculto y con `runasoriginaluser` |
| 4 | Registra el autostart | `HKCU\...\Run` → `CronosPOSAgent` con la ruta **entre comillas dobles** |
| 5 | Lanza el agente | En segundo plano, sin ventana y con `runasoriginaluser` |

**`PrivilegesRequired=admin` + `PrivilegesRequiredOverridesAllowed=dialog commandline`:**
se pide elevación para instalar en Program Files; si no hay credenciales de
administrador, Inno reintenta sin elevar y el destino cae a `C:\ProgramData\CronosAgent`,
que también es permanente y escribible sin admin. El despliegue silencioso sigue
funcionando en ambos casos.

**`runasoriginaluser` es imprescindible** en los pasos 3 y 5: cuando el
instalador corre elevado, `HKCU` y `%LOCALAPPDATA%` son los del administrador y
no los del operador de la caja. Ejecutando el agente como el usuario original,
el token, los certificados y la clave de auto-arranque acaban en el perfil
correcto. Por el mismo motivo la sección `[Registry]` lleva
`Check: not IsAdminInstallMode`: en instalaciones elevadas es el propio agente
quien registra el auto-arranque en la rama correcta durante su primer arranque
(`EnsureAutostartRegistered`).

### Instalación silenciosa por línea de comandos

```bash
CronosAgentSetup-1.4.0.exe /VERYSILENT /SUPPRESSMSGBOXES /NORESTART
```

- `/VERYSILENT`: Sin interfaz gráfica
- `/SUPPRESSMSGBOXES`: Sin diálogos de confirmación
- `/NORESTART`: No reiniciar Windows

### Desinstalación

El desinstalador (generado automáticamente por Inno Setup):
1. Mata el proceso del agente (`taskkill`)
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
| **CP850** (por defecto) | `ESC t 2` | `1B 74 02` | Multilingual Latin-1: Á É Í Ó Ú Ñ ñ Ü ü ¿ ¡ |
| CP858 | `ESC t 19` | `1B 74 13` | CP850 + símbolo € |
| CP1252 | `ESC t 16` | `1B 74 10` | Windows Latin-1 |
| CP437 | `ESC t 0` | `1B 74 00` | USA/Standard Europe (página de fábrica) |
| `none` | — | — | Desactiva todo el tratamiento: los bytes viajan tal cual |

El comando **no** se antepone ciegamente: si el payload empieza por `ESC @`
(`1B 40`, inicializar impresora), la selección se inyecta **después**. `ESC @`
reinicia la impresora y restaura la página de fábrica, así que anteponerla la
anularía y el ticket volvería a imprimir basura.

Si el payload ya trae su propio `ESC t` en la cabecera (primeros 64 bytes), se
asume que el frontend gestiona la codificación y el agente no interfiere.

**2. Transcodificación UTF-8 → bytes de la página de códigos**

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

Las tablas (`escpos_codepages.go`) se generaron a partir de los codecs estándar
`cp850` / `cp858` / `cp1252` / `cp437` y cubren el rango `0x80–0xFF` completo; el
ASCII se copia sin traducir porque es idéntico en las cuatro páginas. Cero
dependencias externas.

Si una runa no existe en la página activa se degrada a su equivalente ASCII
(`asciiFallback`: `Á`→`A`, `—`→`-`, `€`→`EUR`, `…`→`...`) y, como último recurso,
a `?`. Nunca se imprime basura.

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
- Copia tal cual los bytes sueltos que no forman UTF-8 válido.

### Configuración

Global en `config.json` (`escpos_code_page`, `escpos_transcode`) y anulable por
ticket con los campos opcionales `code_page` y `transcode` de `POST /api/print`.
Se aceptan alias: `850`, `pc850`, `latin1`, `windows-1252`, `off`…

### Cobertura de tests

`escpos_test.go` — 14 casos: bytes exactos de las mayúsculas acentuadas en CP850
y CP1252, degradación a ASCII en CP437, integridad de los comandos ESC/POS, logo
raster con UTF-8 incrustado que debe salir intacto, inserción después de `ESC @`,
respeto a un `ESC t` propio del frontend y resolución de alias.

```bash
go test ./...   # ejecutar desde Windows o macOS (el paquete no compila en Linux)
```

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
  "code_page": "cp850",
  "transcode": true
}
```

El `printer_data` del ejemplo es `ESC @` + `"ARTÍCULO ÑOÑO\n"` en UTF-8 + corte
(`GS V 0`). Así lo transforma el agente antes de mandarlo al spooler:

```
Recibido:  1B 40             41 52 54 C3 8D 43 55 4C 4F 20 C3 91 4F C3 91 4F 0A  1D 56 00
Enviado:   1B 40  1B 74 02   41 52 54 D6    43 55 4C 4F 20 A5    4F A5    4F 0A  1D 56 00
                  ^^^^^^^^            ^^                ^^       ^^
                  ESC t 2 tras ESC @  Í en CP850        Ñ en CP850
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

### Pipeline completo de distribución Windows:

```bash
# 1. Compilar binario optimizado
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
  go build -ldflags="-H=windowsgui -w -s" -o build/cronos-pos-agent.exe .

# 2. Generar instalador (ejecutar en Windows)
ISCC.exe installer/setup.iss

# 3. Resultado: installer/Output/CronosAgentSetup-1.4.0.exe
# 4. Despliegue silencioso en cajas de cobro:
#    CronosAgentSetup-1.4.0.exe /VERYSILENT /SUPPRESSMSGBOXES /NORESTART
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
