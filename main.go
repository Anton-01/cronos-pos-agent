package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/getlantern/systray"
)

var startTime time.Time

func main() {
	startTime = time.Now()

	logCloser, err := SetupLogger()
	if err != nil {
		log.Fatalf("Error inicializando logger: %v", err)
	}
	defer logCloser.Close()

	log.Printf("Cronos POS Agent v%s iniciando...", AgentVersion)

	systray.Run(onReady, onExit)
}

func onReady() {
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("Error cargando configuración: %v", err)
	}
	log.Printf("Configuración cargada (%d orígenes CORS)", len(cfg.AllowedOrigins))

	systray.SetTitle("Cronos Agent")
	systray.SetTooltip("Cronos POS Agent v" + AgentVersion)

	mStatus := systray.AddMenuItem("Cronos Agent: Operativo", "Estado del agente")
	mStatus.Disable()

	autostartEnabled := isAutostartEnabled()
	mAutostart := systray.AddMenuItemCheckbox("Iniciar con el Sistema", "Iniciar automáticamente con el sistema", autostartEnabled)

	systray.AddSeparator()

	mQuit := systray.AddMenuItem("Salir", "Cerrar el agente")

	srv := &http.Server{
		Addr:    "127.0.0.1:9100",
		Handler: NewRouter(cfg),
	}

	go func() {
		log.Printf("Servidor HTTP escuchando en http://127.0.0.1:9100")
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
			case <-mQuit.ClickedCh:
				systray.Quit()
			case <-sigChan:
				systray.Quit()
			}
		}
	}()
}

func onExit() {
	log.Println("Cronos Agent finalizado.")
}
