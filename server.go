package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"runtime"
)

func buildOriginsMap(origins []string) map[string]bool {
	m := make(map[string]bool, len(origins))
	for _, o := range origins {
		m[o] = true
	}
	return m
}

func corsMiddleware(allowedOrigins map[string]bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		if allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Cronos-Agent-Token")
			w.Header().Set("Access-Control-Max-Age", "86400")
			w.Header().Set("Vary", "Origin")
		}

		if r.Method == http.MethodOptions {
			if !allowedOrigins[origin] {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func authMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		provided := r.Header.Get("X-Cronos-Agent-Token")
		if provided == "" || provided != token {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Token de autenticación inválido o ausente",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func NewRouter(cfg Config) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/api/health", handleAPIHealth)
	mux.HandleFunc("/api/printers", handlePrinters)
	mux.HandleFunc("/api/print", handlePrint)

	originsMap := buildOriginsMap(cfg.AllowedOrigins)
	return corsMiddleware(originsMap, authMiddleware(cfg.APIToken, mux))
}

func handlePrinters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	printers, err := discoverPrinters()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "No se pudieron obtener las impresoras del sistema",
			"details": err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(printers)
}

func handlePrint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req PrintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "JSON inválido en el cuerpo de la petición",
			"details": err.Error(),
		})
		return
	}

	if req.PrinterName == "" || req.PrinterData == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Los campos 'printer_name' y 'printer_data' son obligatorios",
		})
		return
	}

	rawBytes, err := base64.StdEncoding.DecodeString(req.PrinterData)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "El campo 'printer_data' no es Base64 válido",
			"details": err.Error(),
		})
		return
	}

	if err := rawPrint(req.PrinterName, rawBytes); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "Error al imprimir en la impresora especificada",
			"details": err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Documento enviado a la impresora correctamente",
	})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"service": "cronos-pos-agent",
		"version": AgentVersion,
	})
}

type HealthResponse struct {
	Status       string        `json:"status"`
	Version      string        `json:"version"`
	Platform     string        `json:"platform"`
	MemoryMB     float64       `json:"memory_mb"`
	AllocMB      float64       `json:"alloc_mb"`
	NumGoroutine int           `json:"num_goroutines"`
	Printers     []PrinterInfo `json:"printers"`
	PrinterCount int           `json:"printer_count"`
}

func handleAPIHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	printers, _ := discoverPrinters()

	resp := HealthResponse{
		Status:       "ok",
		Version:      AgentVersion,
		Platform:     runtime.GOOS + "/" + runtime.GOARCH,
		MemoryMB:     float64(mem.Sys) / 1024 / 1024,
		AllocMB:      float64(mem.Alloc) / 1024 / 1024,
		NumGoroutine: runtime.NumGoroutine(),
		Printers:     printers,
		PrinterCount: len(printers),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
