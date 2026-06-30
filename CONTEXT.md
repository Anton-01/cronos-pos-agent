# Cronos POS Agent — Contexto del Proyecto

## Estado Actual

**Fase 5: Expansión Enterprise** — Completada

Fases completadas: 1 (Inicialización), 2 (Autodescubrimiento), 3 (Motor RAW ESC/POS), 4 (Seguridad, Autostart, Build), 5 (CORS dinámico, Health avanzado, Logs con rotación, Auto-updates).

## Arquitectura

### Decisiones Técnicas

| Decisión | Elección | Justificación |
|---|---|---|
| Lenguaje | Go 1.x | Binario estático, concurrencia nativa, cross-compile Windows/Mac |
| System tray | `github.com/getlantern/systray` v1.2.2 | API simple, soporte Windows/Mac/Linux |
| Servidor HTTP | `net/http` (stdlib) | Sin dependencias externas, rendimiento suficiente para agente local |
| CORS | Middleware dinámico desde `config.json` | Orígenes configurables sin recompilar |
| Binding | `127.0.0.1:9100` | Solo loopback, nunca expuesto a la red |
| Auth | Token local UUID v4 + header `X-Cronos-Agent-Token` | Sin servidor externo, generado al primer arranque |
| Printers (Win) | `github.com/alexbrainman/printer` | Acceso al Windows Print Spooler via syscall |
| Printers (Mac) | `lpstat -a` / `lp -d -o raw` (stdlib `os/exec`) | Descubrimiento e impresión CUPS nativa |
| Build Tags | `//go:build windows` / `//go:build darwin` | Compilación condicional por plataforma |
| Impresión (Win) | `printer.Open` + `StartRawDocument` | Inyección RAW directa al Spooler sin filtro de driver |
| Impresión (Mac) | `lp -d <name> -o raw <tmpfile>` | Envío RAW via CUPS con archivo temporal auto-eliminado |
| Autostart (Win) | Registro de Windows `HKCU\...\Run` | Estándar de Windows para apps de usuario |
| Autostart (Mac) | LaunchAgent plist en `~/Library/LaunchAgents` | Estándar de macOS para agentes de usuario |
| Logs | Rotación nativa con `RotatingLogger` | Sin dependencias externas, 10MB max, 3 backups |
| Updates | Goroutine con polling cada 6h | Consulta JSON remoto, estructura lista para descarga binaria |

### Estructura de Archivos

```
cronos-pos-agent/
├── main.go              # Systray (menú, señales OS) + goroutines (HTTP, updater)
├── server.go            # Router, middlewares (CORS dinámico + Auth), handlers
├── config.go            # Carga/generación de config.json, constante AgentVersion
├── logger.go            # RotatingLogger: escritura a archivo con rotación 10MB/3 backups
├── updater.go           # CheckForUpdates: polling de versión contra servidor central
├── printer.go           # Tipos compartidos (PrinterInfo, PrintRequest)
├── printer_windows.go   # Build tag: windows — spooler, RAW print, autostart (registro)
├── printer_darwin.go    # Build tag: darwin — CUPS, RAW print, autostart (launchd)
├── config.json          # (generado en runtime) — NO versionar
├── cronos-agent.log     # (generado en runtime) — NO versionar
├── .gitignore
├── go.mod
├── go.sum
├── CONTEXT.md           # Este archivo — fuente de verdad del proyecto
└── README.md
```

## Archivo `config.json` — Esquema Completo

Generado automáticamente en el primer arranque junto al ejecutable (permisos `0600`):

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
  "update_url": "https://pos-app.tech/agent/version.json"
}
```

| Propiedad | Tipo | Descripción |
|---|---|---|
| `api_token` | `string` | UUID v4 generado con `crypto/rand`. Valida header `X-Cronos-Agent-Token` |
| `allowed_origins` | `string[]` | Lista de orígenes CORS permitidos. Editable sin recompilar |
| `update_url` | `string` | URL del JSON de versión para auto-updates |

Si el archivo ya existe al arrancar, el agente preserva los valores del usuario y solo rellena campos faltantes con valores por defecto.

## Seguridad — Token Local

**Flujo de autenticación:**
1. El frontend React lee el token de `config.json` (o lo recibe del instalador/setup).
2. Toda petición a `/api/*` debe incluir el header `X-Cronos-Agent-Token: <token>`.
3. Si el header falta o no coincide, el agente responde `401 Unauthorized`.
4. El endpoint `/health` está exento de autenticación (permite health checks sin token).

## Endpoints HTTP

Base: `http://127.0.0.1:9100`

| Método | Ruta | Auth | Descripción | Estado |
|---|---|---|---|---|
| `GET` | `/health` | No | Health check básico (status, service, version) | Implementado |
| `GET` | `/api/health` | Si | Health avanzado (RAM, goroutines, impresoras) | Implementado |
| `GET` | `/api/printers` | Si | Lista impresoras instaladas en el SO | Implementado |
| `POST` | `/api/print` | Si | Envía datos RAW (ESC/POS) a una impresora | Implementado |

## Respuesta de `GET /api/health`

```json
{
  "status": "ok",
  "version": "0.2.0",
  "platform": "windows/amd64",
  "memory_mb": 12.45,
  "alloc_mb": 3.21,
  "num_goroutines": 6,
  "printers": [
    { "name": "EPSON_TM_T20III" },
    { "name": "Microsoft Print to PDF" }
  ],
  "printer_count": 2
}
```

| Campo | Tipo | Descripción |
|---|---|---|
| `status` | `string` | Siempre `"ok"` si el agente está respondiendo |
| `version` | `string` | Versión actual del agente (`AgentVersion`) |
| `platform` | `string` | SO y arquitectura (`runtime.GOOS/GOARCH`) |
| `memory_mb` | `float64` | Memoria total del sistema asignada al proceso (`MemStats.Sys`) |
| `alloc_mb` | `float64` | Memoria heap actualmente en uso (`MemStats.Alloc`) |
| `num_goroutines` | `int` | Número de goroutines activas |
| `printers` | `array` | Lista de impresoras detectadas en el SO |
| `printer_count` | `int` | Cantidad de impresoras disponibles |

## CORS — Orígenes Dinámicos

Los orígenes ahora se leen del arreglo `allowed_origins` en `config.json`. El operador puede agregar o quitar dominios sin recompilar el binario.

Headers permitidos en CORS: `Content-Type`, `Authorization`, `X-Cronos-Agent-Token`.

Preflight `OPTIONS` responde `204 No Content` si el origen es válido, `403 Forbidden` si no lo es.

## Sistema de Logs con Rotación

| Parámetro | Valor |
|---|---|
| Archivo | `cronos-agent.log` (junto al ejecutable) |
| Tamaño máximo | 10 MB por archivo |
| Backups máximos | 3 archivos históricos |
| Nomenclatura | `cronos-agent.log.1`, `.2`, `.3` |
| Salida dual | `stdout` + archivo (vía `io.MultiWriter`) |

**Política de retención:** Al alcanzar 10MB, el archivo actual se renombra a `.1`, los existentes rotan (`.1`→`.2`, `.2`→`.3`), y el `.3` anterior se elimina. Esto garantiza un máximo de ~40MB en disco (activo + 3 históricos).

## Auto-Updates

El agente ejecuta `CheckForUpdates()` en una goroutine al iniciar. Consulta `update_url` cada 6 horas esperando un JSON:

```json
{
  "latest_version": "0.3.0",
  "download_url": "https://pos-app.tech/agent/releases/cronos-pos-agent-0.3.0.exe",
  "release_notes": "Mejoras de rendimiento en impresión",
  "mandatory": false
}
```

Actualmente solo registra en el log si hay versión nueva disponible. La descarga y reemplazo del binario está preparada como estructura pero pendiente de implementación.

## Payload de Impresión — `POST /api/print`

```json
{
  "printer_name": "EPSON_TM_T20III",
  "printer_data": "G0BIb2xhIE11bmRvIQ=="
}
```

| Campo | Tipo | Descripción |
|---|---|---|
| `printer_name` | `string` | Nombre exacto de la impresora tal como aparece en `GET /api/printers` |
| `printer_data` | `string` | Comandos ESC/POS codificados en Base64 estándar (RFC 4648) |

**Respuestas:**

| Código | Significado |
|---|---|
| `200` | `{"status":"ok","message":"Documento enviado a la impresora correctamente"}` |
| `400` | JSON inválido, campos faltantes, o Base64 malformado |
| `401` | Token ausente o inválido en header `X-Cronos-Agent-Token` |
| `500` | Error del spooler/impresora (nombre no encontrado, conexión fallida, etc.) |

## Menú Systray

1. **"Cronos Agent: Operativo"** — Deshabilitado (solo indicador visual)
2. **"Iniciar con el Sistema"** — Checkbox funcional:
   - **Windows:** Escribe/borra clave `CronosPOSAgent` en `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
   - **Mac:** Crea/elimina plist `com.cronos.pos-agent.plist` en `~/Library/LaunchAgents`
3. Separador
4. **"Salir"** — Cierra el agente

## Dependencias Externas

| Módulo | Versión | Uso |
|---|---|---|
| `github.com/getlantern/systray` | v1.2.2 | Icono y menú en barra de tareas |
| `github.com/alexbrainman/printer` | v0.0.0-20200912 | Windows Print Spooler (descubrimiento + impresión RAW) |
| `golang.org/x/sys` | v0.1.0+ | Acceso al Registro de Windows (`windows/registry`) |

## Compilación para Producción

### Desde macOS (M4 Pro) hacia Windows x64:

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
  go build -ldflags="-H=windowsgui -w -s" -o cronos-pos-agent.exe .
```

### Desde macOS (M4 Pro) nativo para Mac:

```bash
GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 \
  go build -ldflags="-w -s" -o cronos-pos-agent .
```

### Prerequisitos en macOS:

```bash
brew install mingw-w64          # Compilador cruzado para Windows
xcode-select --install          # Xcode CLT para compilación nativa Mac
```

## Fases — Historial Completo

### Fase 1: Inicialización ✓
### Fase 2: Autodescubrimiento ✓
### Fase 3: Motor RAW ESC/POS ✓
### Fase 4: Seguridad, Autostart, Build ✓
### Fase 5: Expansión Enterprise ✓
- ~~CORS dinámico desde `config.json`~~ ✓
- ~~Endpoint `GET /api/health` con métricas de runtime~~ ✓
- ~~Logs con rotación (10MB, 3 backups)~~ ✓
- ~~Arquitectura de auto-updates (polling cada 6h)~~ ✓

### Pendiente (fuera de scope actual)
- Comunicación bidireccional (WebSocket/SSE)
- Descarga automática de binarios en auto-update
- Firma de binarios (code signing)
- Instalador/Desinstalador empaquetado
