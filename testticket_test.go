package main

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
)

// sampleTechnicalInfo is the printer the ticket tests describe.
func sampleTechnicalInfo() PrinterTechnicalInfo {
	return PrinterTechnicalInfo{
		Name:      "POS-80 Termica",
		Driver:    "Generic / Text Only",
		Port:      "USB001",
		Processor: "Windows x64",
		IsDefault: true,
		QueuedIDs: 2,
	}
}

// The ticket exists to be photographed and read, so everything a support case
// needs has to be on it: which printer, which driver, which port, and which
// encoding was in force when it printed.
func TestBuildTestTicketShowsTechnicalData(t *testing.T) {
	info := sampleTechnicalInfo()
	body := string(BuildTestTicket(info, DefaultEncodingOptions(), PrinterProfile{}, 42, 9100))

	for _, want := range []string{
		info.Name, info.Driver, info.Port, info.Processor,
		AgentVersion, "9100",
		"cp858", "ESC t 19",
		"TICKET DE PRUEBA",
		"sin calibrar",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("el ticket no menciona %q", want)
		}
	}
}

// A verified printer has to be reported as such: it is the difference between
// "these accents are missing on purpose" and "this printer is misconfigured".
func TestBuildTestTicketReportsVerifiedProfile(t *testing.T) {
	profile := PrinterProfile{CodePage: "cp858", Verified: true, Source: profileSourceCalibration}
	body := string(BuildTestTicket(sampleTechnicalInfo(), DefaultEncodingOptions(), profile, 42, 9100))

	if !strings.Contains(body, "calibrado, verificado") {
		t.Error("el ticket no informa de que la impresora está calibrada y verificada")
	}
}

// Whatever could not be read is printed as an explicit notice: a blank field
// would look like the agent never asked.
func TestBuildTestTicketPrintsNotes(t *testing.T) {
	info := sampleTechnicalInfo()
	info.Notes = []string{"no se pudo leer el puerto"}

	body := string(BuildTestTicket(info, DefaultEncodingOptions(), PrinterProfile{}, 42, 9100))
	if !strings.Contains(body, "no se pudo leer el puerto") {
		t.Error("el ticket no imprime los avisos de lo que no se pudo determinar")
	}
}

// The character sample is the point of the ticket: every class a Spanish
// receipt can carry has to be on it, so that a photo shows which of them the
// printer gets wrong.
func TestBuildTestTicketCoversTheCharacterSet(t *testing.T) {
	body := string(BuildTestTicket(sampleTechnicalInfo(), DefaultEncodingOptions(), PrinterProfile{}, 42, 9100))

	for _, want := range []string{
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ",
		"abcdefghijklmnopqrstuvwxyz",
		"0123456789",
		"á é í ó ú",
		"Á É Í Ó Ú",
		"ñ", "Ñ", "¿", "¡", "€", "º", "ª", "°",
		"Michoacán",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("el ticket no incluye la muestra %q", want)
		}
	}
}

// The ticket has to advance the paper past the blade and then cut: the cutter
// sits a few millimetres above the print head, so cutting without feeding takes
// the last lines with it.
func TestBuildTestTicketFeedsAndCuts(t *testing.T) {
	body := BuildTestTicket(sampleTechnicalInfo(), DefaultEncodingOptions(), PrinterProfile{}, 42, 9100)

	if !bytes.HasSuffix(body, escCutPaper) {
		t.Error("el ticket no termina con el corte de papel")
	}
	if !bytes.HasSuffix(body[:len(body)-len(escCutPaper)], []byte("\n\n\n\n")) {
		t.Error("el ticket no avanza el papel antes de cortar: se llevaría las últimas líneas")
	}
}

// The ruler is how the operator finds out how many columns their printer has,
// so it has to be exactly as long as the width it claims to measure.
func TestColumnRulerMatchesWidth(t *testing.T) {
	for _, columns := range []int{32, 42, 48} {
		if got := len(columnRuler(columns)); got != columns {
			t.Errorf("columnRuler(%d) mide %d caracteres", columns, got)
		}
		if got := len(columnScale(columns)); got != columns {
			t.Errorf("columnScale(%d) mide %d caracteres", columns, got)
		}
	}

	// Los múltiplos de diez llevan su número y los de cinco su marca.
	ruler := columnRuler(20)
	if !strings.Contains(ruler, "10") || !strings.Contains(ruler, "20") {
		t.Errorf("la regla no marca las decenas: %q", ruler)
	}
	if !strings.Contains(ruler, "+") {
		t.Errorf("la regla no marca los cincos: %q", ruler)
	}
}

// A width outside the range a thermal printer can have is a mistake in the
// request, and answering it with a broken layout would hide that. The builder
// falls back to the 80 mm default instead.
func TestBuildTestTicketClampsColumns(t *testing.T) {
	for _, columns := range []int{0, -5, 3, 500} {
		body := string(BuildTestTicket(sampleTechnicalInfo(), DefaultEncodingOptions(), PrinterProfile{}, columns, 9100))
		if !strings.Contains(body, "42 columnas") {
			t.Errorf("con columns=%d el ticket no volvió al ancho por defecto", columns)
		}
	}
}

// No printed line may run past the configured width, or the printer wraps it
// and the technical block stops lining up. Long driver names are the case that
// breaks it, so one is used here.
func TestBuildTestTicketRespectsLineWidth(t *testing.T) {
	const columns = 32

	info := sampleTechnicalInfo()
	info.Driver = "Controlador generico de impresora termica de 80 milimetros"

	body := BuildTestTicket(info, DefaultEncodingOptions(), PrinterProfile{}, columns, 9100)

	// The formatting commands are stripped first: they occupy no column on
	// paper, so counting them would flag lines that print perfectly well.
	for _, line := range strings.Split(stripESCPOSCommands(string(body)), "\n") {
		if n := len([]rune(line)); n > columns {
			t.Errorf("línea de %d caracteres con un ancho de %d: %q", n, columns, line)
		}
	}
}

// The ticket has to survive the encoding pipeline that every real receipt goes
// through: its commands intact, its text transcoded, and no byte left that a
// printer on another code page would turn into a drawing character.
func TestBuildTestTicketSurvivesTheEncodingPipeline(t *testing.T) {
	body := BuildTestTicket(sampleTechnicalInfo(), DefaultEncodingOptions(), PrinterProfile{}, 42, 9100)

	payload, err := BuildESCPOSPayload(body, DefaultEncodingOptions())
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	if !bytes.HasPrefix(payload, []byte{0x1B, 0x40, 0x1B, 0x74, 0x13}) {
		t.Errorf("el ticket no abre con ESC @ + ESC t 19: % X", payload[:5])
	}

	// Los comandos de formato tienen que llegar enteros al papel.
	for name, command := range map[string][]byte{
		"negrita":  escBoldOn,
		"subrayad": escUnderlineOn,
		"centrado": escAlignCenter,
		"doble":    escSizeDouble,
		"corte":    escCutPaper,
	} {
		if !bytes.Contains(payload, command) {
			t.Errorf("la transcodificación se comió el comando de %s (% X)", name, command)
		}
	}

	// Y el texto tiene que leerse igual en cualquiera de las tres tablas que
	// puede tener activa la impresora, que es la garantía del modo compatible.
	text := payload[5:]
	for _, page := range []*charmap.Charmap{codePageCP437, codePageCP850, codePageCP858} {
		decoded, err := page.NewDecoder().Bytes(text)
		if err != nil {
			t.Fatalf("no se pudo decodificar: %v", err)
		}
		if !bytes.Contains(decoded, []byte("Michoacán")) {
			t.Errorf("leído como %s, el ticket pierde los acentos seguros", pageName(page))
		}
		if bytes.Contains(decoded, []byte("╡")) {
			t.Errorf("leído como %s, el ticket imprime el símbolo de dibujo que la v1.8.0 sacaba", pageName(page))
		}
	}
}

// stripESCPOSCommands removes the escape sequences the test ticket uses, so a
// line's printed width can be measured.
func stripESCPOSCommands(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		switch {
		case s[i] == 0x1B && i+2 < len(s): // ESC x n
			i += 3
		case s[i] == 0x1D && i+2 < len(s): // GS x n
			i += 3
		default:
			out.WriteByte(s[i])
			i++
		}
	}
	return out.String()
}
