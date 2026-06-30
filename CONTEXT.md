# Cronos POS Agent — Contexto del Proyecto

## Estado Actual

**Fase 2: Autodescubrimiento de Impresoras** — Completado

Fase 1 (Inicialización) completada. Fase 2 agrega autodescubrimiento de impresoras del sistema operativo mediante build tags por plataforma.

## Arquitectura

### Decisiones Técnicas

| Decisión | Elección | Justificación |
|---|---|---|
| Lenguaje | Go 1.x | Binario estático, concurrencia nativa, cross-compile Windows/Mac |
| System tray | `github.com/getlantern/systray` v1.2.2 | API simple, soporte Windows/Mac/Linux |
| Servidor HTTP | `net/http` (stdlib) | Sin dependencias externas, rendimiento suficiente para agente local |
| CORS | Middleware custom | Control estricto de orígenes permitidos |
| Binding | `127.0.0.1:9100` | Solo loopback, nunca expuesto a la red |
| Printers (Win) | `github.com/alexbrainman/printer` | Acceso al Windows Print Spooler via syscall |
| Printers (Mac) | `lpstat -a` (stdlib `os/exec`) | Comando CUPS nativo, sin dependencias externas |
| Build Tags | `//go:build windows` / `//go:build darwin` | Compilación condicional por plataforma |

### Estructura de Archivos

```
cronos-pos-agent/
├── main.go              # Systray (menú, señales OS) + goroutine del servidor HTTP
├── server.go            # Router, middleware CORS, handlers de endpoints
├── printer.go           # Tipo PrinterInfo compartido entre plataformas
├── printer_windows.go   # Build tag: windows — descubrimiento via Win32 Spooler
├── printer_darwin.go    # Build tag: darwin — descubrimiento via lpstat (CUPS)
├── go.mod
├── go.sum
├── CONTEXT.md           # Este archivo — fuente de verdad del proyecto
└── README.md
```

## Endpoints HTTP

Base: `http://127.0.0.1:9100`

| Método | Ruta | Descripción | Estado |
|---|---|---|---|
| `GET` | `/health` | Health check del agente | Implementado |
| `GET` | `/api/printers` | Lista impresoras instaladas en el SO | Implementado |

## CORS — Orígenes Permitidos

| Origen | Propósito |
|---|---|
| `https://pos-app.tech` | Producción (SaaS) |
| `http://localhost:3000` | Desarrollo local |
| `http://localhost:5173` | Desarrollo local (Vite) |
| `http://127.0.0.1:3000` | Desarrollo local |
| `http://127.0.0.1:5173` | Desarrollo local (Vite) |

Preflight `OPTIONS` responde `204 No Content` si el origen es válido, `403 Forbidden` si no lo es.

## Menú Systray

1. **"Cronos Agent: Operativo"** — Deshabilitado (solo indicador visual)
2. **"Iniciar con el Sistema"** — Checkbox toggle (lógica de persistencia pendiente)
3. Separador
4. **"Salir"** — Cierra el agente

## Dependencias Externas

| Módulo | Versión | Uso |
|---|---|---|
| `github.com/getlantern/systray` | v1.2.2 | Icono y menú en barra de tareas |
| `github.com/alexbrainman/printer` | v0.0.0-20200912 | Lectura del Windows Print Spooler |

## Fases Pendientes

### Fase 2: Impresión (parcialmente completada)
- ~~Descubrimiento de impresoras del sistema (Windows/Mac)~~ ✓
- Endpoint `POST /print` que reciba payload con datos del ticket
- Renderizado de ticket (texto plano, ESC/POS, o HTML→PDF)
- Envío directo a impresora seleccionada

### Fase 3: Comunicación Bidireccional
- WebSocket o SSE para push desde el agente al SaaS
- Notificación de estado de impresión (éxito/error)
- Heartbeat periódico al SaaS

### Fase 4: Distribución y Auto-actualización
- Build cross-platform (Windows `.exe` + Mac `.app`)
- Installer/Uninstaller
- Mecanismo de auto-actualización (check de versión contra endpoint remoto)
- Firma de binarios (code signing)
- Registro como servicio/startup (persistencia del checkbox "Iniciar con el Sistema")
