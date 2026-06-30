package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

type VersionInfo struct {
	LatestVersion string `json:"latest_version"`
	DownloadURL   string `json:"download_url"`
	ReleaseNotes  string `json:"release_notes"`
	Mandatory     bool   `json:"mandatory"`
}

func CheckForUpdates(updateURL string) {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	checkOnce(updateURL)

	for range ticker.C {
		checkOnce(updateURL)
	}
}

func checkOnce(updateURL string) {
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get(updateURL)
	if err != nil {
		log.Printf("[updater] Error consultando actualizaciones: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[updater] Servidor respondió con código %d", resp.StatusCode)
		return
	}

	var remote VersionInfo
	if err := json.NewDecoder(resp.Body).Decode(&remote); err != nil {
		log.Printf("[updater] Error parseando respuesta de versión: %v", err)
		return
	}

	if remote.LatestVersion == AgentVersion {
		log.Printf("[updater] Agente actualizado (v%s)", AgentVersion)
		return
	}

	log.Printf("[updater] Nueva versión disponible: v%s (actual: v%s)", remote.LatestVersion, AgentVersion)
	if remote.ReleaseNotes != "" {
		log.Printf("[updater] Notas: %s", remote.ReleaseNotes)
	}

	// TODO: Implementar descarga automática del binario
	// 1. Descargar desde remote.DownloadURL a un archivo temporal
	// 2. Verificar checksum/firma del binario descargado
	// 3. Reemplazar el ejecutable actual (renombrar viejo, mover nuevo)
	// 4. Reiniciar el agente
}
