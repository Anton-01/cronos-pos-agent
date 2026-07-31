package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const AgentVersion = "1.4.0"

// configFileName es el nombre del archivo de configuración dentro de agentDir().
const configFileName = "config.json"

type Config struct {
	APIToken       string   `json:"api_token"`
	AllowedOrigins []string `json:"allowed_origins"`
	UpdateURL      string   `json:"update_url"`
	Port           int      `json:"port"`
	// ESCPOSCodePage es la página de códigos que se activa en la ticketera
	// antes de cada impresión RAW (cp850, cp858, cp1252, cp437 o none).
	ESCPOSCodePage string `json:"escpos_code_page"`
	// ESCPOSTranscode activa la conversión de texto UTF-8 a los bytes de esa
	// página de códigos. Puntero para distinguir "false" de "no configurado".
	ESCPOSTranscode *bool `json:"escpos_transcode"`
	// Autostart guarda la preferencia de arranque con el sistema. El agente
	// repara la entrada del registro en cada arranque sólo si es true.
	Autostart *bool `json:"autostart"`
}

var defaultOrigins = []string{
	"https://pos-app.tech",
	"http://localhost:3000",
	"http://localhost:5173",
	"http://127.0.0.1:3000",
	"http://127.0.0.1:5173",
}

const defaultUpdateURL = "https://pos-app.tech/agent/version.json"

var (
	appConfig     Config
	configMu      sync.Mutex
	configOnce    sync.Once
	configLoadErr error
)

func boolPtr(v bool) *bool { return &v }

func LoadConfig() (Config, error) {
	configOnce.Do(func() {
		configPath := configFilePath()
		needsWrite := false

		data, err := os.ReadFile(configPath)
		if err == nil {
			if err := json.Unmarshal(data, &appConfig); err != nil {
				configLoadErr = fmt.Errorf("config.json corrupto: %w", err)
				return
			}
		}

		if appConfig.APIToken == "" {
			token, err := generateUUID()
			if err != nil {
				configLoadErr = fmt.Errorf("error generando token: %w", err)
				return
			}
			appConfig.APIToken = token
			needsWrite = true
		}

		if len(appConfig.AllowedOrigins) == 0 {
			appConfig.AllowedOrigins = defaultOrigins
			needsWrite = true
		}

		if appConfig.UpdateURL == "" {
			appConfig.UpdateURL = defaultUpdateURL
			needsWrite = true
		}

		if appConfig.Port == 0 {
			appConfig.Port = defaultPort
			needsWrite = true
		}

		if appConfig.ESCPOSCodePage == "" {
			appConfig.ESCPOSCodePage = defaultCodePage
			needsWrite = true
		}

		if appConfig.ESCPOSTranscode == nil {
			appConfig.ESCPOSTranscode = boolPtr(true)
			needsWrite = true
		}

		if appConfig.Autostart == nil {
			appConfig.Autostart = boolPtr(true)
			needsWrite = true
		}

		if needsWrite {
			if err := saveConfig(configPath); err != nil {
				configLoadErr = err
			}
		}
	})

	configMu.Lock()
	defer configMu.Unlock()
	return appConfig, configLoadErr
}

// SetAutostartPreference persiste en config.json si el usuario quiere que el
// agente arranque con el sistema. El agente sólo repara la entrada del registro
// de Windows cuando esta preferencia está activa, de forma que desmarcar la
// opción del systray no se revierta en el siguiente arranque.
func SetAutostartPreference(enabled bool) error {
	if _, err := LoadConfig(); err != nil {
		return err
	}

	configMu.Lock()
	defer configMu.Unlock()

	appConfig.Autostart = boolPtr(enabled)
	return saveConfig(configFilePath())
}

// AutostartPreferred indica si la preferencia guardada permite registrar el
// auto-arranque. Ante cualquier error de lectura se asume que sí: un agente de
// punto de venta debe estar siempre disponible tras un reinicio.
func AutostartPreferred() bool {
	cfg, err := LoadConfig()
	if err != nil || cfg.Autostart == nil {
		return true
	}
	return *cfg.Autostart
}

// EncodingOptionsFor combina la configuración global de codificación ESC/POS
// con los overrides opcionales que traiga la petición de impresión.
func EncodingOptionsFor(reqCodePage string, reqTranscode *bool) EncodingOptions {
	opts := EncodingOptions{CodePage: defaultCodePage, Transcode: true}

	if cfg, err := LoadConfig(); err == nil {
		if cfg.ESCPOSCodePage != "" {
			// Un valor inválido en config.json no debe tumbar cada impresión:
			// se avisa en el log y se sigue con la página por defecto.
			if _, _, err := ResolveCodePage(cfg.ESCPOSCodePage); err != nil {
				log.Printf("[escpos] %v — se usa '%s'", err, defaultCodePage)
			} else {
				opts.CodePage = cfg.ESCPOSCodePage
			}
		}
		if cfg.ESCPOSTranscode != nil {
			opts.Transcode = *cfg.ESCPOSTranscode
		}
	}

	if reqCodePage != "" {
		opts.CodePage = reqCodePage
	}
	if reqTranscode != nil {
		opts.Transcode = *reqTranscode
	}

	return opts
}

func saveConfig(path string) error {
	jsonData, err := json.MarshalIndent(appConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("error serializando config: %w", err)
	}
	if err := os.WriteFile(path, jsonData, 0600); err != nil {
		return fmt.Errorf("error escribiendo config.json: %w", err)
	}
	return nil
}

// configFilePath devuelve la ruta de config.json dentro del directorio de datos
// del agente (ver agentDir() en paths_windows.go / paths_darwin.go).
func configFilePath() string {
	return filepath.Join(agentDir(), configFileName)
}

func generateUUID() (string, error) {
	var uuid [16]byte
	if _, err := rand.Read(uuid[:]); err != nil {
		return "", err
	}
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16]), nil
}
