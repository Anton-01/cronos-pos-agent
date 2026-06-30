package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/getlantern/systray"
)

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetTitle("Cronos Agent")
	systray.SetTooltip("Cronos POS Agent")

	mStatus := systray.AddMenuItem("Cronos Agent: Operativo", "Estado del agente")
	mStatus.Disable()

	mAutostart := systray.AddMenuItemCheckbox("Iniciar con el Sistema", "Iniciar automáticamente con el sistema", false)

	systray.AddSeparator()

	mQuit := systray.AddMenuItem("Salir", "Cerrar el agente")

	srv := &http.Server{
		Addr:    "127.0.0.1:9100",
		Handler: NewRouter(),
	}

	go func() {
		log.Printf("Servidor HTTP escuchando en http://127.0.0.1:9100")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Error al iniciar servidor HTTP: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for {
			select {
			case <-mAutostart.ClickedCh:
				if mAutostart.Checked() {
					mAutostart.Uncheck()
				} else {
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
