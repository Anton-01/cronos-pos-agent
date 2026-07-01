# Cronos POS Agent — Contexto del Proyecto

## Estado Actual

**Fase 7: Conversión Gráfica y Soporte PDF** — Completado

Fases completadas: 1 (Inicialización), 2 (Autodescubrimiento), 3 (Motor RAW ESC/POS), 4 (Seguridad, Autostart, Build), 5 (CORS dinámico, Health, Monitoreo de cola), 6 (Port fallback, Self-healing, Certificados SSL nativos, Instalador Inno Setup), 7 (Impresión nativa de PDF en impresoras convencionales).

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
| Cola (Mac) | `lpstat -W not-completed -o` | Consulta CUPS nativa de trabajos pendientes |
| Build Tags | `//go:build windows` / `//go:build darwin` | Compilación condicional por plataforma |
| Autostart (Win) | Registro de Windows `HKCU\...\Run` | Estándar de Windows para apps de usuario |
| Autostart (Mac) | LaunchAgent plist en `~/Library/LaunchAgents` | Estándar de macOS para agentes de usuario |
| Logs | Rotación nativa con `RotatingLogger` | Sin dependencias externas, 10MB max, 3 backups |
| Updates | Goroutine con polling cada 6h | Consulta JSON remoto |
| Instalador | Inno Setup 6.x | Instalador silencioso estándar de Windows, sin admin |

### Estructura de Archivos

```
cronos-pos-agent/
├── main.go              # Entry point: flags CLI, self-healing, systray, goroutines
├── server.go            # Router, middlewares (CORS dinámico + Auth), handlers (6 endpoints)
├── config.go            # Carga/generación de config.json, constante AgentVersion (1.3.0)
├── network.go           # ResolvePort: fallback dinámico de puertos con scan
├── certs.go             # GenerateCerts: RSA 2048 + X.509 autofirmado nativo
├── logger.go            # RotatingLogger: escritura a archivo con rotación 10MB/3 backups
├── updater.go           # CheckForUpdates: polling de versión contra servidor central
├── printer.go           # Tipos compartidos (PrinterInfo, PrintRequest, QueueInfo, PrintJob)
├── printer_windows.go   # Build tag: windows — spooler, RAW, cola, autostart, killOrphan
├── printer_darwin.go    # Build tag: darwin — CUPS, RAW, cola, autostart, killOrphan
├── installer/
│   └── setup.iss        # Script Inno Setup para instalador silencioso Windows
├── config.json          # (generado en runtime) — NO versionar
├── cronos-agent.log     # (generado en runtime) — NO versionar
├── private-key.pem      # (generado con --generate-certs) — NO versionar
├── digital-certificate.txt  # (generado con --generate-certs) — NO versionar
├── .gitignore
├── go.mod
├── go.sum
├── CONTEXT.md           # Este archivo — fuente de verdad del proyecto
└── README.md
```

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
  "port": 9100
}
```

| Propiedad | Tipo | Default | Descripción |
|---|---|---|---|
| `api_token` | `string` | UUID v4 auto | Token de autenticación para header `X-Cronos-Agent-Token` |
| `allowed_origins` | `string[]` | 5 orígenes | Lista de orígenes CORS permitidos |
| `update_url` | `string` | pos-app.tech | URL del JSON de versión para auto-updates |
| `port` | `int` | `9100` | Puerto preferido. Si está ocupado, busca el siguiente libre (9101–9110) |

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

## Flags de Línea de Comandos

| Flag | Descripción |
|---|---|
| `--generate-certs` | Genera `private-key.pem` y `digital-certificate.txt` en la carpeta del ejecutable y sale |
| (sin flags) | Modo normal: arranca systray + servidor HTTP |

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

**Herramienta recomendada:** [Inno Setup 6.x](https://jrsoftware.org/isinfo.php) — gratuito, ligero, estándar de la industria para apps de escritorio Windows. Elegido sobre NSIS (sintaxis más compleja) y WiX (sobredimensionado para un agente single-binary).

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
| 2 | Copia binario | A `%LOCALAPPDATA%\CronosAgent\cronos-pos-agent.exe` |
| 3 | Genera certificados SSL | Ejecuta `--generate-certs` en modo oculto |
| 4 | Inyecta registro de autostart | `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` → `CronosPOSAgent` |
| 5 | Lanza el agente | Ejecuta el binario en segundo plano (sin ventana) |

### Instalación silenciosa por línea de comandos

```bash
CronosAgentSetup-1.3.0.exe /VERYSILENT /SUPPRESSMSGBOXES /NORESTART
```

- `/VERYSILENT`: Sin interfaz gráfica
- `/SUPPRESSMSGBOXES`: Sin diálogos de confirmación
- `/NORESTART`: No reiniciar Windows

### Desinstalación

El desinstalador (generado automáticamente por Inno Setup):
1. Mata el proceso del agente
2. Elimina la clave del registro de autostart
3. Limpia `config.json`, logs, certificados y binario
4. Elimina la carpeta si queda vacía

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
| `POST` | `/api/print` | Si | Envía datos RAW (ESC/POS) a una impresora |
| `POST` | `/api/print/pdf` | Si | Imprime un archivo PDF en una impresora convencional |

## Dependencias Externas

| Módulo | Versión | Uso |
|---|---|---|
| `github.com/getlantern/systray` | v1.2.2 | Icono y menú en barra de tareas |
| `github.com/alexbrainman/printer` | v0.0.0-20200912 | Windows Print Spooler |
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

# 3. Resultado: installer/Output/CronosAgentSetup-1.3.0.exe
# 4. Despliegue silencioso en cajas de cobro:
#    CronosAgentSetup-1.3.0.exe /VERYSILENT /SUPPRESSMSGBOXES /NORESTART
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
