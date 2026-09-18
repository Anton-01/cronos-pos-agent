package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"runtime"
	"time"
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

// authMiddleware guards every handler it wraps: the request must carry a valid
// X-Cronos-Agent-Token header or it is rejected with 401. It deliberately knows
// nothing about paths — deciding which routes are public and which are guarded
// is the router's job (see NewRouter).
func authMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

// newPublicMux builds the discovery surface: the routes that must answer
// without a token. The POS frontend pings them to find out whether the agent is
// running on port 9100 before it has any token to send, so putting them behind
// the auth middleware would make discovery impossible (it used to answer 401).
// Nothing here touches the printers or reveals local configuration.
func newPublicMux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/api/health", handleAPIHealth)

	return mux
}

// newProtectedMux builds the working surface: every route that reaches the
// local hardware. It is never mounted on its own — NewRouter always puts the
// auth middleware in front of it.
func newProtectedMux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/printers", handlePrinters)
	mux.HandleFunc("/api/printers/queue", handlePrinterQueue)
	mux.HandleFunc("/api/print", handlePrint)
	mux.HandleFunc("/api/print/pdf", handlePrintPDF)
	mux.HandleFunc("/api/print/calibrate", handleCalibrate)
	mux.HandleFunc("/api/print/calibrate/confirm", handleCalibrateConfirm)

	return mux
}

func NewRouter(cfg Config) http.Handler {
	root := newPublicMux()

	// The whole /api/ subtree hangs behind the token check, and ServeMux
	// resolves the most specific pattern first: /api/health keeps hitting the
	// public handler while /api/print, /api/printers and friends land in the
	// guarded mux. Mounting the subtree instead of each route one by one also
	// makes the default fail-closed — a new /api/ endpoint is protected the
	// moment it is registered, even if someone forgets about this file.
	root.Handle("/api/", authMiddleware(cfg.APIToken, newProtectedMux()))

	// CORS wraps the entire server, public routes included: without the
	// Access-Control-Allow-Origin header the browser blocks the discovery ping
	// before the agent ever gets to answer it.
	originsMap := buildOriginsMap(cfg.AllowedOrigins)
	return corsMiddleware(originsMap, root)
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
	Status        string  `json:"status"`
	Version       string  `json:"version"`
	UptimeSeconds int     `json:"uptime_seconds"`
	MemoryUsageMB float64 `json:"memory_usage_mb"`
}

func handleAPIHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	allocMB := float64(mem.Alloc) / 1024 / 1024
	allocMB = math.Round(allocMB*100) / 100

	resp := HealthResponse{
		Status:        "ok",
		Version:       AgentVersion,
		UptimeSeconds: int(time.Since(startTime).Seconds()),
		MemoryUsageMB: allocMB,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
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

func handlePrinterQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	printerName := r.URL.Query().Get("printer_name")
	if printerName == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "El parámetro 'printer_name' es obligatorio",
		})
		return
	}

	queue, err := queryPrintQueue(printerName)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "Error consultando la cola de impresión",
			"details": err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(queue)
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

	// La página de códigos ESC/POS sale de config.json y puede sobrescribirse
	// por ticket con los campos opcionales "code_page" y "transcode".
	if req.CodePage != "" {
		if _, _, err := ResolveCodePage(req.CodePage); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error":   "Página de códigos ESC/POS no soportada",
				"details": err.Error(),
			})
			return
		}
	}
	enc := EncodingOptionsFor(req.PrinterName, req.CodePage, req.Transcode)

	if err := rawPrint(req.PrinterName, rawBytes, enc); err != nil {
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

func handlePrintPDF(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req PDFPrintRequest
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

	pdfBytes, err := base64.StdEncoding.DecodeString(req.PrinterData)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "El campo 'printer_data' no es Base64 válido",
			"details": err.Error(),
		})
		return
	}

	if err := printPDF(req.PrinterName, pdfBytes); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "Error al imprimir el PDF en la impresora especificada",
			"details": err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "PDF enviado a la impresora correctamente",
	})
}

// handleCalibrate prints the accent calibration ticket on a printer and answers
// with the list of numbered options it carries.
//
// It exists because ESC/POS is a one-way protocol as far as encoding goes: a
// printer can be told to select a code page but never asked which one it has,
// so the only way to establish what a given piece of hardware does with
// "ESC t n" is to print every candidate and have a person read the paper. The
// operator looks for the line where "ÁÉÍÓÚ" is printed as letters instead of
// box-drawing symbols and sends its number to /api/print/calibrate/confirm.
//
// Nothing is saved here: printing the ticket changes no configuration, so
// calibrating a printer twice is harmless.
func handleCalibrate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CalibrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "JSON inválido en el cuerpo de la petición", err)
		return
	}
	if req.PrinterName == "" {
		writeJSONError(w, http.StatusBadRequest, "El campo 'printer_name' es obligatorio", nil)
		return
	}

	// The ticket carries its own "ESC t n" before every sample line — that is
	// the whole point of it — so it is written to the spooler exactly as built,
	// with the encoding pipeline disabled. Letting the agent re-encode it would
	// mean measuring the agent instead of the printer.
	if err := rawPrint(req.PrinterName, BuildCalibrationTicket(), EncodingOptions{CodePage: codePageNone}); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Error al imprimir el ticket de calibración", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       "ok",
		"message":      "Ticket de calibración enviado. Confirme el número de la línea correcta.",
		"printer_name": req.PrinterName,
		"options":      CalibrationOptions(),
		"profile":      PrinterProfileFor(req.PrinterName),
	})
}

// handleCalibrateConfirm stores the result the operator read off the calibration
// ticket, which is what verifies a printer and unlocks the four characters
// compatibility mode was folding (Á Í Ó Ú).
//
// Option 0 is the other useful answer: it says no line printed correctly, which
// is a real outcome on a printer whose font ROM has no uppercase accented vowels
// at all. It is stored as an unverified profile, leaving the printer in
// compatibility mode for good — "Animo" is a worse ticket than "Ánimo" but a far
// better one than "╡nimo".
func handleCalibrateConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CalibrationConfirmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "JSON inválido en el cuerpo de la petición", err)
		return
	}
	if req.PrinterName == "" {
		writeJSONError(w, http.StatusBadRequest, "El campo 'printer_name' es obligatorio", nil)
		return
	}

	profile := PrinterProfile{
		CodePage:   defaultCodePage,
		CodePageID: req.CodePageID,
		Verified:   false,
		Source:     profileSourceCalibration,
	}

	if req.Option != 0 {
		chosen, ok := calibrationOptionByNumber(req.Option)
		if !ok {
			writeJSONError(w, http.StatusBadRequest,
				fmt.Sprintf("La opción %d no existe en el ticket de calibración", req.Option), nil)
			return
		}
		profile.CodePage = chosen.CodePage
		profile.Verified = true

		// The line printed without any "ESC t n" is the one that identifies a
		// printer ignoring the command. Its page is still the right one to
		// encode against —it is what the firmware decodes with— so the profile
		// records it and simply keeps sending the standard selector, which that
		// printer will go on ignoring at no cost.
		if chosen.Selector != nil && req.CodePageID == nil {
			profile.CodePageID = chosen.Selector
		}
	}

	if err := SavePrinterProfile(req.PrinterName, profile); err != nil {
		writeJSONError(w, http.StatusBadRequest, "No se pudo guardar el perfil de la impresora", err)
		return
	}

	message := "Impresora verificada: se imprimirán todos los acentos."
	if !profile.Verified {
		message = "Impresora sin página de códigos utilizable: se mantiene el modo compatible (Á Í Ó Ú se imprimirán sin acento)."
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       "ok",
		"message":      message,
		"printer_name": req.PrinterName,
		"profile":      profile,
	})
}

// calibrationOptionByNumber looks up one line of the calibration ticket by the
// number printed next to it.
func calibrationOptionByNumber(option int) (CalibrationOption, bool) {
	for _, candidate := range CalibrationOptions() {
		if candidate.Option == option {
			return candidate, true
		}
	}
	return CalibrationOption{}, false
}

// writeJSONError answers with the {"error", "details"} shape every handler in
// this file uses, so that a new endpoint cannot drift into its own.
func writeJSONError(w http.ResponseWriter, status int, message string, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	body := map[string]string{"error": message}
	if err != nil {
		body["details"] = err.Error()
	}
	json.NewEncoder(w).Encode(body)
}
