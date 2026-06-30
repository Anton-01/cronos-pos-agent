# Cronos POS Agent — Contexto del Proyecto

## Estado Actual

**Fase 4: Producción y Cierre** — Completado

Fases completadas: 1 (Inicialización), 2 (Autodescubrimiento), 3 (Motor RAW ESC/POS), 4 (Seguridad, Autostart, Build).

## Arquitectura

### Decisiones Técnicas

| Decisión | Elección | Justificación |
|---|---|---|
| Lenguaje | Go 1.x | Binario estático, concurrencia nativa, cross-compile Windows/Mac |
| System tray | `github.com/getlantern/systray` v1.2.2 | API simple, soporte Windows/Mac/Linux |
| Servidor HTTP | `net/http` (stdlib) | Sin dependencias externas, rendimiento suficiente para agente local |
| CORS | Middleware custom | Control estricto de orígenes permitidos |
| Binding | `127.0.0.1:9100` | Solo loopback, nunca expuesto a la red |
| Auth | Token local UUID v4 + header `X-Cronos-Agent-Token` | Sin servidor externo, generado al primer arranque |
| Printers (Win) | `github.com/alexbrainman/printer` | Acceso al Windows Print Spooler via syscall |
| Printers (Mac) | `lpstat -a` / `lp -d -o raw` (stdlib `os/exec`) | Descubrimiento e impresión CUPS nativa |
| Build Tags | `//go:build windows` / `//go:build darwin` | Compilación condicional por plataforma |
| Impresión (Win) | `printer.Open` + `StartRawDocument` | Inyección RAW directa al Spooler sin filtro de driver |
| Impresión (Mac) | `lp -d <name> -o raw <tmpfile>` | Envío RAW via CUPS con archivo temporal auto-eliminado |
| Autostart (Win) | Registro de Windows `HKCU\...\Run` | Estándar de Windows para apps de usuario |
| Autostart (Mac) | LaunchAgent plist en `~/Library/LaunchAgents` | Estándar de macOS para agentes de usuario |

### Estructura de Archivos

```
cronos-pos-agent/
├── main.go              # Systray (menú, señales OS) + goroutine del servidor HTTP
├── server.go            # Router, middlewares (CORS + Auth), handlers de endpoints
├── config.go            # Carga/generación de config.json con token UUID v4
├── printer.go           # Tipos compartidos (PrinterInfo, PrintRequest)
├── printer_windows.go   # Build tag: windows — spooler, RAW print, autostart (registro)
├── printer_darwin.go    # Build tag: darwin — CUPS, RAW print, autostart (launchd)
├── config.json          # (generado en runtime) Token API local — NO versionar
├── go.mod
├── go.sum
├── CONTEXT.md           # Este archivo — fuente de verdad del proyecto
└── README.md
```

## Seguridad — Token Local

Al primer arranque, el agente genera un UUID v4 criptográficamente seguro y lo persiste en `config.json` (permisos `0600`) junto al ejecutable:

```json
{
  "api_token": "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"
}
```

**Flujo de autenticación:**
1. El frontend React lee el token de `config.json` (o lo recibe del instalador/setup).
2. Toda petición a `/api/*` debe incluir el header `X-Cronos-Agent-Token: <token>`.
3. Si el header falta o no coincide, el agente responde `401 Unauthorized`.
4. El endpoint `/health` está exento de autenticación (permite health checks sin token).

## Endpoints HTTP

Base: `http://127.0.0.1:9100`

| Método | Ruta | Auth | Descripción | Estado |
|---|---|---|---|---|
| `GET` | `/health` | No | Health check del agente | Implementado |
| `GET` | `/api/printers` | Si | Lista impresoras instaladas en el SO | Implementado |
| `POST` | `/api/print` | Si | Envía datos RAW (ESC/POS) a una impresora | Implementado |

## CORS — Orígenes Permitidos

| Origen | Propósito |
|---|---|
| `https://pos-app.tech` | Producción (SaaS) |
| `http://localhost:3000` | Desarrollo local |
| `http://localhost:5173` | Desarrollo local (Vite) |
| `http://127.0.0.1:3000` | Desarrollo local |
| `http://127.0.0.1:5173` | Desarrollo local (Vite) |

Headers permitidos en CORS: `Content-Type`, `Authorization`, `X-Cronos-Agent-Token`.

Preflight `OPTIONS` responde `204 No Content` si el origen es válido, `403 Forbidden` si no lo es.

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

El agente decodifica `printer_data` a `[]byte` y los inyecta como documento RAW al spooler/CUPS, sin que el driver altere los bytes. Esto preserva comandos ESC/POS (corte de papel, negritas, códigos de barras, etc.).

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

| Flag | Efecto |
|---|---|
| `GOOS=windows GOARCH=amd64` | Cross-compile hacia Windows 64-bit |
| `CGO_ENABLED=1` | Requerido por `systray` (usa CGO para APIs nativas del SO) |
| `CC=x86_64-w64-mingw32-gcc` | Compilador cruzado MinGW (instalar con `brew install mingw-w64`) |
| `-H=windowsgui` | Subsistema GUI de Windows — elimina la consola negra al ejecutar |
| `-w` | Omite tabla DWARF de debug — reduce tamaño del binario |
| `-s` | Omite tabla de símbolos — reduce tamaño adicional |

### Desde macOS (M4 Pro) nativo para Mac:

```bash
GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 \
  go build -ldflags="-w -s" -o cronos-pos-agent .
```

### Prerequisitos en macOS:

```bash
# Compilador cruzado para Windows
brew install mingw-w64

# Xcode Command Line Tools (para compilación nativa Mac)
xcode-select --install
```

## Fases — Todas Completadas

### Fase 1: Inicialización
- ~~Módulo Go, systray, servidor HTTP, CORS~~ ✓

### Fase 2: Autodescubrimiento
- ~~Endpoint `GET /api/printers` con build tags~~ ✓

### Fase 3: Motor RAW
- ~~Endpoint `POST /api/print` con inyección ESC/POS directa~~ ✓

### Fase 4: Producción y Cierre
- ~~Seguridad por token local (UUID v4 + middleware auth)~~ ✓
- ~~Auto-arranque Windows (Registro `HKCU\...\Run`)~~ ✓
- ~~Auto-arranque Mac (LaunchAgent plist)~~ ✓
- ~~Instrucciones de compilación optimizada~~ ✓

### Pendiente (fuera de scope actual)
- Comunicación bidireccional (WebSocket/SSE)
- Auto-actualización del binario
- Firma de binarios (code signing)
- Instalador/Desinstalador empaquetado
