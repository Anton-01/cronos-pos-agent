package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/atotto/clipboard"
	"github.com/getlantern/systray"
)

var startTime time.Time

// shutdownTimeout es lo que se espera a que terminen las peticiones en curso
// antes de cerrar el servidor a la fuerza. Un ticket tarda milisegundos en
// llegar al spooler; 5 segundos cubren de sobra el peor caso sin dejar colgado
// al operador que acaba de pulsar "Salir".
const shutdownTimeout = 5 * time.Second

// httpServer guarda el servidor en marcha para poder cerrarlo desde onExit(),
// que systray ejecuta en otra goroutine. Es atómico porque quien lo escribe
// (onReady) y quien lo lee (onExit) son goroutines distintas.
var httpServer atomic.Pointer[http.Server]

// agentDone se cierra en onExit para que las goroutines de larga vida (polling
// de actualizaciones, bucle del menú) terminen en vez de quedar colgadas.
var (
	agentDone = make(chan struct{})
	exitOnce  sync.Once
)

func main() {
	generateCerts := flag.Bool("generate-certs", false, "Genera certificados SSL (private-key.pem y digital-certificate.txt) y sale")
	disableAutostartFlag := flag.Bool("disable-autostart", false, "Elimina el auto-arranque del sistema y sale (usado por el desinstalador)")
	noInstall := flag.Bool("no-install", false, "No reubicar el binario a la ruta permanente (uso en desarrollo)")
	relaunched := flag.Bool("relaunched", false, "Uso interno: el agente ya fue relanzado desde la ruta permanente")
	firstRun := flag.Bool("first-run", false, "Muestra la ventana de bienvenida post-instalación (lo usa el instalador al terminar)")
	flushRestart := flag.Bool(flushRestartFlagName, false, "Uso interno: limpieza profunda antes de arrancar, tras un reinicio pedido desde el System Tray")
	flag.Parse()

	if *generateCerts {
		if err := GenerateCerts(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	if *disableAutostartFlag {
		if err := SetAutostartPreference(false); err != nil {
			fmt.Fprintf(os.Stderr, "Advertencia: no se pudo guardar la preferencia: %v\n", err)
		}
		if err := disableAutostart(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	startTime = time.Now()

	logCloser, err := SetupLogger()
	if err != nil {
		log.Fatalf("Error inicializando logger: %v", err)
	}
	defer logCloser.Close()

	log.Printf("Cronos POS Agent v%s iniciando...", AgentVersion)

	// This instance was spawned by "Reiniciar y Limpiar (Debug)": the cleanup
	// runs here, before anything binds a socket or paints an icon, so that the
	// port left by the previous process is free and no disposable file survives
	// into the new session. It is also placed before killOrphanInstances()
	// because its wait gives the predecessor the time it needs to die on its
	// own, leaving nothing to kill.
	if *flushRestart {
		RunFlushCleanup()
	}

	killOrphanInstances()

	// El binario debe vivir siempre en su ruta fija y permanente. Si se lanzó
	// desde una carpeta volátil se copia allí, se relanza y esta instancia
	// termina: así la entrada de auto-arranque nunca apunta a un archivo que
	// pueda desaparecer. El flag --relaunched corta cualquier bucle.
	if !*noInstall && !*relaunched {
		// El flag de bienvenida viaja con el relanzado: si el instalador acaba
		// de lanzar el binario desde una carpeta temporal, la ventana debe
		// abrirla la instancia definitiva, no la que está a punto de morir.
		var passthrough []string
		if *firstRun {
			passthrough = append(passthrough, "--first-run")
		}
		if EnsurePermanentLocation(passthrough...) {
			log.Println("Instancia temporal finalizada tras reubicar el agente")
			return
		}
	}

	// Repara la entrada de auto-arranque en cada arranque (ruta permanente y
	// entre comillas dobles), salvo que el usuario la haya desactivado.
	EnsureAutostartRegistered()

	// La ventana de bienvenida corre en su propia goroutine con su propio bucle
	// de mensajes: systray.Run() se queda con el hilo principal y no volvería
	// hasta que el usuario cierre el agente.
	if *firstRun {
		go ShowFirstRunWelcome()
	}

	systray.Run(onReady, onExit)
}

func onReady() {
	// Estado neutro desde el primer instante: el agente está vivo pero todavía
	// no acepta trabajos. El icono (el gato tuxedo) va embebido en el
	// ejecutable, no en un archivo junto a él, así que sobrevive a las
	// actualizaciones, que sustituyen el .exe entero.
	systray.SetIcon(trayIconStarting())
	systray.SetTitle("Cronos Agent")
	systray.SetTooltip(fmt.Sprintf("Cronos POS Agent v%s — iniciando…", AgentVersion))

	mStatus := systray.AddMenuItem("Cronos Agent: Iniciando…", "Estado del agente")
	mStatus.Disable()

	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("Error cargando configuración: %v", err)
	}
	log.Printf("Configuración cargada (%d orígenes CORS)", len(cfg.AllowedOrigins))

	port, err := ResolvePort(cfg.Port)
	if err != nil {
		log.Fatalf("Error resolviendo puerto: %v", err)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)

	autostartEnabled := isAutostartEnabled()
	mAutostart := systray.AddMenuItemCheckbox("Iniciar con el Sistema", "Iniciar automáticamente con el sistema", autostartEnabled)

	mCopyToken := systray.AddMenuItem("Copiar Token de Seguridad", "Copia el token del agente al portapapeles")

	systray.AddSeparator()

	// Placed right above "Salir" on purpose: both items end this process, and
	// keeping them together separates the two destructive actions of the menu
	// from the everyday ones.
	mRestart := systray.AddMenuItem("Reiniciar y Limpiar (Debug)", "Limpia los archivos temporales y reinicia el agente por completo")

	mQuit := systray.AddMenuItem("Salir", "Cerrar el agente")

	srv := &http.Server{
		Addr:    addr,
		Handler: NewRouter(cfg),
	}

	// El puerto se abre aquí y no dentro de ListenAndServe: el icono verde debe
	// significar "el socket está aceptando conexiones", no "hemos lanzado una
	// goroutine que quizá lo consiga". Si el bind falla, el icono se queda gris
	// y el motivo queda en el log.
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Error abriendo el puerto %s: %v", addr, err)
	}
	httpServer.Store(srv)

	go func() {
		log.Printf("Servidor HTTP escuchando en http://%s", addr)
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			// El servidor ha muerto sin que nadie lo cerrase: el agente ya no
			// acepta tickets, así que el icono vuelve al estado neutro.
			systray.SetIcon(trayIconStarting())
			systray.SetTooltip(fmt.Sprintf("Cronos POS Agent v%s — detenido", AgentVersion))
			mStatus.SetTitle("Cronos Agent: Detenido")
			log.Printf("Servidor HTTP detenido: %v", err)
		}
	}()

	// A partir de aquí el agente ya escucha: verde.
	systray.SetIcon(trayIconReady())
	systray.SetTooltip(fmt.Sprintf("Cronos POS Agent v%s — Operativo (:%d)", AgentVersion, port))
	mStatus.SetTitle(fmt.Sprintf("Cronos Agent: Operativo (:%d)", port))

	go CheckForUpdates(cfg.UpdateURL, agentDone)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for {
			select {
			case <-mAutostart.ClickedCh:
				if mAutostart.Checked() {
					if err := disableAutostart(); err != nil {
						log.Printf("Error desactivando auto-arranque: %v", err)
					}
					mAutostart.Uncheck()
					persistAutostartPreference(false)
				} else {
					if err := enableAutostart(); err != nil {
						log.Printf("Error activando auto-arranque: %v", err)
					}
					mAutostart.Check()
					persistAutostartPreference(true)
				}
			case <-mCopyToken.ClickedCh:
				copyTokenToClipboard()
			case <-mRestart.ClickedCh:
				log.Println("Reinicio con limpieza solicitado desde el menú del System Tray")
				// The tray goes back to its neutral state first: from this
				// point on the agent is on its way out, and a green icon would
				// be claiming a socket that is about to close.
				systray.SetIcon(trayIconStarting())
				systray.SetTooltip(fmt.Sprintf("Cronos POS Agent v%s — reiniciando…", AgentVersion))
				mStatus.SetTitle("Cronos Agent: Reiniciando…")

				// RestartWithFlush does not return when it succeeds: it ends
				// this process once the successor is running. An error means no
				// successor was spawned, so the agent must stay up —a till with
				// no agent is worse than a till that did not restart— and the
				// tray is restored to its operational state.
				if err := RestartWithFlush(); err != nil {
					log.Printf("Error reiniciando el agente: %v", err)
					systray.SetIcon(trayIconReady())
					systray.SetTooltip(fmt.Sprintf("Cronos POS Agent v%s — Operativo (:%d)", AgentVersion, port))
					mStatus.SetTitle(fmt.Sprintf("Cronos Agent: Operativo (:%d)", port))
				}
			case <-mQuit.ClickedCh:
				log.Println("Salida solicitada desde el menú del System Tray")
				systray.Quit() // systray llama a onExit(), que cierra el servidor
			case sig := <-sigChan:
				log.Printf("Señal %s recibida, cerrando el agente", sig)
				systray.Quit()
			case <-agentDone:
				// El agente ya se está cerrando: esta goroutine termina en vez
				// de quedarse esperando en canales que nadie volverá a usar.
				return
			}
		}
	}()
}

// onExit lo invoca systray cuando el bucle de la bandeja termina: al pulsar
// "Salir", al recibir SIGINT/SIGTERM o al cerrar la sesión de Windows. Es el
// único punto de cierre del agente, así que aquí se liberan los recursos.
func onExit() {
	// sync.Once porque cerrar dos veces un canal entra en pánico: la salida
	// puede llegar por el menú, por una señal o por el cierre de sesión de
	// Windows, y no conviene depender de que systray las serialice.
	exitOnce.Do(func() {
		close(agentDone) // detiene el polling de actualizaciones y el bucle del menú
		shutdownHTTPServer()
		log.Println("Cronos Agent finalizado.")
	})
}

// shutdownHTTPServer cierra ordenadamente el servidor HTTP y, con él, el socket
// de escucha: sin esto el puerto podría quedar ocupado el tiempo que tarde el
// sistema en reciclarlo y el siguiente arranque caería al puerto 9101, con el
// frontend apuntando todavía al 9100.
//
// Shutdown() espera a que terminen las peticiones en curso —un ticket a medio
// enviar al spooler no se corta por la mitad— con un límite de 5 segundos, tras
// el cual se cierra a la fuerza. El Swap(nil) lo hace idempotente.
func shutdownHTTPServer() {
	srv := httpServer.Swap(nil)
	if srv == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("El cierre ordenado no terminó a tiempo (%v), se fuerza", err)
		srv.Close()
		return
	}
	log.Println("Servidor HTTP cerrado y puerto liberado")
}

// persistAutostartPreference guarda en config.json la decisión del usuario para
// que EnsureAutostartRegistered no vuelva a crear la entrada del registro en el
// siguiente arranque si la desactivó a propósito.
func persistAutostartPreference(enabled bool) {
	if err := SetAutostartPreference(enabled); err != nil {
		log.Printf("Error guardando la preferencia de auto-arranque: %v", err)
	}
}

// copyTokenToClipboard lee el api_token guardado en config.json y lo copia al
// portapapeles del sistema operativo usando la librería multiplataforma
// github.com/atotto/clipboard (pbcopy en macOS, API nativa en Windows).
func copyTokenToClipboard() {
	cfg, err := LoadConfig()
	if err != nil {
		log.Printf("Error leyendo config.json para copiar el token: %v", err)
		return
	}

	if cfg.APIToken == "" {
		log.Println("No hay token disponible en config.json para copiar")
		return
	}

	if err := clipboard.WriteAll(cfg.APIToken); err != nil {
		log.Printf("Error copiando el token al portapapeles: %v", err)
		return
	}

	log.Println("Token copiado al portapapeles")
}
