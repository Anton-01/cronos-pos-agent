package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const AgentVersion = "1.9.0"

// configFileName es el nombre del archivo de configuración dentro de agentDir().
const configFileName = "config.json"

// configSchemaVersion identifica la forma del config.json escrito por esta
// versión del agente. Sirve para aplicar migraciones una sola vez sobre los
// archivos ya existentes en las cajas de cobro, sin tocar el api_token.
const configSchemaVersion = 3

// legacyDefaultCodePage y legacyDefaultCodePageV2 son las páginas que las
// versiones anteriores escribían en el config.json del primer arranque: CP850
// hasta la v1.4.0 y CP1252 de la v1.5.0 a la v1.8.0. Al no haberlas elegido
// nadie, se migran al valor por defecto actual (ver migrateConfigSchema).
const (
	legacyDefaultCodePage   = "cp850"
	legacyDefaultCodePageV2 = "cp1252"
)

type Config struct {
	// SchemaVersion es la versión del esquema de este archivo, no la del
	// agente. Ausente (0) en los config.json escritos hasta la v1.4.0.
	SchemaVersion  int      `json:"config_version"`
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
	// ESCPOSStripAccents folds EVERY diacritic of the ticket text, so that
	// "Ánimo" is printed as "Animo" and "Michoacán" as "Michoacan". Desde la
	// v1.9.0 está desactivado por defecto: el modo compatible
	// (ESCPOSCompatibility) consigue lo mismo sin sacrificar "á é í ó ú ñ ¿ ¡",
	// y este plegado total queda solo para la impresora que no imprima
	// correctamente ni siquiera el subconjunto seguro. Puntero para distinguir
	// "false" de "no configurado"; por defecto false.
	ESCPOSStripAccents *bool `json:"strip_accents"`
	// ESCPOSInitialize prepends "ESC @" (0x1B 0x40) to every RAW job, so that
	// the printer starts from a known state and no leftover setting from the
	// previous ticket survives into this one. Pointer for the same reason;
	// default true.
	ESCPOSInitialize *bool `json:"escpos_initialize"`
	// ESCPOSCompatibility restringe el ticket a los caracteres que PC437, PC850
	// y PC858 codifican con el mismo byte, plegando a ASCII los cuatro que sí
	// difieren (Á Í Ó Ú). Es lo que hace que un ticket sea correcto en una
	// impresora cuya página activa se desconoce (ver escpos_compat.go). Puntero
	// para distinguir "false" de "no configurado"; por defecto true, y se apaga
	// solo en las impresoras con perfil verificado.
	ESCPOSCompatibility *bool `json:"escpos_compatibility"`
	// Printers guarda lo que el agente sabe de cada impresora: la página de
	// códigos que de verdad decodifica y si está verificada. La clave es el
	// nombre de la cola tal y como lo muestra el sistema. Una impresora sin
	// entrada aquí imprime en modo compatible, que es correcto en cualquier
	// hardware (ver printer_profiles.go).
	Printers map[string]PrinterProfile `json:"printers,omitempty"`
	// ESCPOSCodePageID sustituye el "n" del comando "ESC t n" por un valor
	// concreto (0–255), manteniendo la tabla de transcodificación de
	// ESCPOSCodePage. Sólo hace falta en ticketeras clónicas que numeran sus
	// páginas de códigos de forma distinta al estándar de Epson. Ausente o
	// null = numeración estándar.
	ESCPOSCodePageID *int `json:"escpos_code_page_id,omitempty"`
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

// LoadConfig reads config.json once and fills in every key the file does not
// carry yet, writing the result back to disk. A missing file is not an error:
// the read fails, every default applies and the agent creates the file on its
// first start with the values above (see the defaults for port, code page,
// transcoding, accent folding and printer reset). New keys added by a later
// version of the agent land in an existing config.json the same way, without
// touching the api_token already handed to the frontend.
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

		if appConfig.ESCPOSStripAccents == nil {
			// Por defecto ya no se pliegan todos los acentos: de eso se encarga
			// el modo compatible, que solo pliega los cuatro caracteres que la
			// impresora podría equivocar y deja intactos "á é í ó ú ñ ¿ ¡".
			appConfig.ESCPOSStripAccents = boolPtr(false)
			needsWrite = true
		}

		if appConfig.ESCPOSCompatibility == nil {
			appConfig.ESCPOSCompatibility = boolPtr(true)
			needsWrite = true
		}

		if appConfig.ESCPOSInitialize == nil {
			appConfig.ESCPOSInitialize = boolPtr(true)
			needsWrite = true
		}

		if appConfig.Autostart == nil {
			appConfig.Autostart = boolPtr(true)
			needsWrite = true
		}

		if appConfig.SchemaVersion < configSchemaVersion {
			migrateConfigSchema()
			appConfig.SchemaVersion = configSchemaVersion
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

// migrateConfigSchema actualiza en sitio un config.json escrito por una versión
// anterior del agente. Se ejecuta dentro de LoadConfig, una sola vez, y nunca
// toca el api_token: perderlo obligaría a reconfigurar cada caja de cobro.
//
// La regla de toda migración es la misma: solo se toca el valor que escribió el
// propio agente como defecto de su época. Cualquier otro lo puso una persona
// mirando un ticket, y eso se respeta.
//
// v1 -> v2: la página de códigos pasó de CP850 a CP1252.
//
// v2 -> v3: la página vuelve a la familia DOS (PC858) y entra el modo
// compatible. CP1252 resultó ser la peor opción posible cuando la impresora
// ignora "ESC t n" —no comparte ni un byte con PC437, así que se pierden todos
// los acentos y no solo los cuatro conflictivos—, y el plegado total de acentos
// que la acompañaba dejaba de imprimir "Michoacán" para no equivocar "Ánimo".
// El modo compatible resuelve las dos cosas a la vez, así que la migración
// también apaga ese plegado total cuando sigue con el valor que puso el agente.
func migrateConfigSchema() {
	page := strings.ToLower(strings.TrimSpace(appConfig.ESCPOSCodePage))

	if page == legacyDefaultCodePage || page == legacyDefaultCodePageV2 {
		appConfig.ESCPOSCodePage = defaultCodePage
		log.Printf("[config] Página de códigos migrada de %s a %s", page, defaultCodePage)
	}

	if appConfig.SchemaVersion >= 2 && appConfig.ESCPOSStripAccents != nil && *appConfig.ESCPOSStripAccents {
		appConfig.ESCPOSStripAccents = boolPtr(false)
		log.Printf("[config] Plegado total de acentos desactivado: lo sustituye el modo compatible")
	}

	if appConfig.ESCPOSCompatibility == nil {
		appConfig.ESCPOSCompatibility = boolPtr(true)
	}
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

// EncodingOptionsFor decide cómo se codifica un ticket concreto, combinando
// cuatro fuentes en orden creciente de autoridad:
//
//  1. los valores por defecto del agente (DefaultEncodingOptions),
//  2. la configuración global de config.json,
//  3. el perfil de ESA impresora, si el agente ya sabe qué página decodifica
//     de verdad (calibración o modelo conocido; ver printer_profiles.go),
//  4. los campos opcionales que traiga la petición de impresión.
//
// El perfil es el que desbloquea "Á Í Ó Ú": mientras la impresora no esté
// verificada el modo compatible los pliega a ASCII, porque enviar su byte sería
// apostar a ciegas por la tabla que tenga cargada el firmware. En cuanto se
// sabe cuál es, el plegado sobra y las cuatro se imprimen de verdad.
func EncodingOptionsFor(printerName string, reqCodePage string, reqTranscode *bool) EncodingOptions {
	opts := DefaultEncodingOptions()

	// compatibilityPinned recuerda si una persona fijó el modo compatible en
	// config.json. Si lo hizo, ni el perfil de la impresora lo cambia: haber
	// escrito la clave a mano es una decisión, y el perfil solo es una
	// deducción del agente.
	compatibilityPinned := false

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
		if cfg.ESCPOSStripAccents != nil {
			opts.StripAccents = *cfg.ESCPOSStripAccents
		}
		if cfg.ESCPOSCompatibility != nil {
			opts.Compatibility = *cfg.ESCPOSCompatibility
			compatibilityPinned = true
		}
		if cfg.ESCPOSInitialize != nil {
			opts.Initialize = *cfg.ESCPOSInitialize
		}
		if cfg.ESCPOSCodePageID != nil {
			id := *cfg.ESCPOSCodePageID
			if id < 0 || id > 255 {
				log.Printf("[escpos] escpos_code_page_id=%d fuera de rango (0–255), se ignora", id)
			} else {
				selector := byte(id)
				opts.SelectorOverride = &selector
			}
		}
	}

	// El perfil de la impresora manda sobre la configuración global: la página
	// de códigos es una propiedad del hardware, y una caja con dos ticketeras
	// distintas necesita una respuesta distinta para cada una.
	if profile := PrinterProfileFor(printerName); profile.CodePage != "" {
		if _, _, err := ResolveCodePage(profile.CodePage); err != nil {
			log.Printf("[escpos] perfil de '%s' inválido: %v — se ignora", printerName, err)
		} else {
			opts.CodePage = profile.CodePage
			opts.SelectorOverride = nil
			if profile.CodePageID != nil && *profile.CodePageID >= 0 && *profile.CodePageID <= 255 {
				selector := byte(*profile.CodePageID)
				opts.SelectorOverride = &selector
			}
			if profile.Verified && !compatibilityPinned {
				opts.Compatibility = false
			}
		}
	}

	if reqCodePage != "" {
		opts.CodePage = reqCodePage
		// El selector forzado describe cómo numera la ticketera UNA página
		// concreta, la configurada. Si el ticket pide otra, ese número deja de
		// ser válido y se vuelve a la numeración estándar.
		opts.SelectorOverride = nil
		// Pedir una página por ticket es una instrucción explícita, y el modo
		// compatible codifica siempre en PC858: dejarlo puesto convertiría esa
		// instrucción en papel mojado sin decirlo en ninguna parte.
		opts.Compatibility = false
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
