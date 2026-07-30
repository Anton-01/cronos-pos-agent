package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/atotto/clipboard"
	"github.com/getlantern/systray"
)

var startTime time.Time

func main() {
	generateCerts := flag.Bool("generate-certs", false, "Genera certificados SSL (private-key.pem y digital-certificate.txt) y sale")
	flag.Parse()

	if *generateCerts {
		if err := GenerateCerts(); err != nil {
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

	killOrphanInstances()

	systray.Run(onReady, onExit)
}

func onReady() {
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

	systray.SetTitle("Cronos Agent")
	systray.SetTooltip(fmt.Sprintf("Cronos POS Agent v%s — :%d", AgentVersion, port))

	mStatus := systray.AddMenuItem(fmt.Sprintf("Cronos Agent: Operativo (:%d)", port), "Estado del agente")
	mStatus.Disable()

	autostartEnabled := isAutostartEnabled()
	mAutostart := systray.AddMenuItemCheckbox("Iniciar con el Sistema", "Iniciar automáticamente con el sistema", autostartEnabled)

	mCopyToken := systray.AddMenuItem("Copiar Token de Seguridad", "Copia el token del agente al portapapeles")

	systray.AddSeparator()

	mQuit := systray.AddMenuItem("Salir", "Cerrar el agente")

	srv := &http.Server{
		Addr:    addr,
		Handler: NewRouter(cfg),
	}

	go func() {
		log.Printf("Servidor HTTP escuchando en http://%s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Error al iniciar servidor HTTP: %v", err)
		}
	}()

	go CheckForUpdates(cfg.UpdateURL)

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
				} else {
					if err := enableAutostart(); err != nil {
						log.Printf("Error activando auto-arranque: %v", err)
					}
					mAutostart.Check()
				}
			case <-mCopyToken.ClickedCh:
				copyTokenToClipboard()
			case <-mQuit.ClickedCh:
				systray.Quit()
			case <-sigChan:
				systray.Quit()
			}
		}
	}()
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

func onExit() {
	log.Println("Cronos Agent finalizado.")
}
