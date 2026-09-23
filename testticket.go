package main

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

// Self-test ticket.
//
// What it is for. When a till prints garbage, the question is always the same
// and always hard to answer over the phone: what does this printer actually do?
// The operator can describe the symptom but not the driver, the port, the code
// page in force or whether the printer was ever calibrated. This ticket puts all
// of it on paper, next to a sample of every character a Spanish receipt can
// contain, so that a photo of it is a complete diagnosis.
//
// It deliberately goes through the very same encoding pipeline as a real ticket
// (BuildESCPOSPayload, with the options that printer would really get). A test
// that bypassed the pipeline would prove nothing about the tickets the shop
// actually prints.

// PrinterTechnicalInfo is what the operating system knows about one printer.
// Every field is best-effort: a driver that cannot be queried prints as "—"
// rather than failing the ticket, because a partial diagnosis is still a
// diagnosis and this is the one path that has to work on a misbehaving printer.
type PrinterTechnicalInfo struct {
	Name      string `json:"name"`
	Driver    string `json:"driver,omitempty"`
	Port      string `json:"port,omitempty"`
	Processor string `json:"processor,omitempty"`
	IsDefault bool   `json:"is_default"`
	QueuedIDs int    `json:"queued_jobs"`
	// Notes carries whatever could not be determined, so the ticket can say
	// "no se pudo leer el controlador" instead of leaving a silent blank.
	Notes []string `json:"notes,omitempty"`
}

// Test ticket column widths. A thermal printer's line length in Font A is a
// property of the paper, not of the protocol: 32 characters on 58 mm rolls and
// 42 on 80 mm ones (48 on some 80 mm models). The agent cannot ask the printer,
// so the ticket prints a ruler and lets the operator read the answer off it.
const (
	testTicketDefaultColumns = 42
	testTicketMinColumns     = 24
	testTicketMaxColumns     = 64
)

// ESC/POS formatting commands used by the test ticket. They are listed here
// rather than inlined so that the ticket body reads as a document and not as a
// stream of magic numbers.
var (
	escAlignLeft    = []byte{0x1B, 0x61, 0x00} // ESC a 0
	escAlignCenter  = []byte{0x1B, 0x61, 0x01} // ESC a 1
	escAlignRight   = []byte{0x1B, 0x61, 0x02} // ESC a 2
	escBoldOn       = []byte{0x1B, 0x45, 0x01} // ESC E 1
	escBoldOff      = []byte{0x1B, 0x45, 0x00} // ESC E 0
	escUnderlineOn  = []byte{0x1B, 0x2D, 0x01} // ESC - 1
	escUnderlineOff = []byte{0x1B, 0x2D, 0x00} // ESC - 0
	escSizeNormal   = []byte{0x1D, 0x21, 0x00} // GS ! 0  (1x1)
	escSizeDouble   = []byte{0x1D, 0x21, 0x11} // GS ! 17 (2x alto y ancho)
	escSizeWide     = []byte{0x1D, 0x21, 0x10} // GS ! 16 (2x ancho)
	escSizeTall     = []byte{0x1D, 0x21, 0x01} // GS ! 1  (2x alto)
	escCutPaper     = []byte{0x1D, 0x56, 0x00} // GS V 0  (corte total)
)

// ticketBuilder accumulates the ticket body. It is a thin wrapper over a byte
// slice whose only job is to keep the document below readable: every line is one
// call, and the column width is remembered instead of being threaded through
// every helper.
type ticketBuilder struct {
	out     []byte
	columns int
}

func (b *ticketBuilder) raw(data []byte) { b.out = append(b.out, data...) }
func (b *ticketBuilder) text(s string)   { b.out = append(b.out, []byte(s)...) }
func (b *ticketBuilder) line(s string)   { b.text(s); b.out = append(b.out, '\n') }
func (b *ticketBuilder) blank()          { b.out = append(b.out, '\n') }
func (b *ticketBuilder) rule()           { b.line(strings.Repeat("-", b.columns)) }
func (b *ticketBuilder) doubleRule()     { b.line(strings.Repeat("=", b.columns)) }
func (b *ticketBuilder) styled(on, off []byte, s string) {
	b.raw(on)
	b.line(s)
	b.raw(off)
}

// field prints one "etiqueta : valor" row, padding the label so that the values
// line up in a column. A value that does not fit wraps onto the next line,
// indented under the first.
//
// Everything here counts runes, never bytes. Padding by len(label) looks right
// in the source and prints wrong the moment a label carries an accent: "Versión"
// is seven characters on paper and eight bytes in memory, so byte padding puts
// its colon one column to the left of every other row in the block.
func (b *ticketBuilder) field(label, value string) {
	const labelWidth = 13

	if value == "" {
		value = "—"
	}

	padded := label
	if pad := labelWidth - len([]rune(padded)); pad > 0 {
		padded += strings.Repeat(" ", pad)
	}

	head := padded + ": "
	indent := strings.Repeat(" ", len([]rune(head)))
	room := b.columns - len([]rune(head))
	if room < 1 {
		b.line(head + value)
		return
	}

	for i, chunk := range wrapValue(value, room) {
		if i == 0 {
			b.line(head + chunk)
			continue
		}
		b.line(indent + chunk)
	}
}

// wrapValue breaks a field's value into chunks of at most width runes,
// preferring word boundaries. A single token longer than the line —a driver
// path, a UNC share— is split hard, because leaving it whole would push the
// line past the paper and let the printer wrap it wherever it liked.
func wrapValue(value string, width int) []string {
	chunks := make([]string, 0, 2)

	for _, line := range wrapText(value, width) {
		runes := []rune(line)
		for len(runes) > width {
			chunks = append(chunks, string(runes[:width]))
			runes = runes[width:]
		}
		chunks = append(chunks, string(runes))
	}

	return chunks
}

// paragraph prints running text wrapped to the ticket's width.
//
// The prose of this ticket cannot be written as fixed lines: the same text has
// to fit a 32-column roll and a 42-column one, and a line hardcoded for the
// wider paper is silently re-wrapped by the printer on the narrower one, which
// breaks every column the technical block lines up. Wrapping here keeps the
// layout a property of the ticket instead of a property of the paper it was
// written for.
func (b *ticketBuilder) paragraph(text string) {
	for _, line := range wrapText(text, b.columns) {
		b.line(line)
	}
}

// wrapText breaks a string into lines of at most width runes, on word
// boundaries. A single word longer than the line is left whole here; the
// callers that cannot afford the overflow pass it through wrapValue, which
// splits it.
func wrapText(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}

	lines := make([]string, 0, 4)
	current := words[0]
	for _, word := range words[1:] {
		if len([]rune(current))+1+len([]rune(word)) <= width {
			current += " " + word
			continue
		}
		lines = append(lines, current)
		current = word
	}
	return append(lines, current)
}

// section prints a centred, emphasised heading.
func (b *ticketBuilder) section(title string) {
	b.blank()
	b.raw(escBoldOn)
	b.line(title)
	b.raw(escBoldOff)
	b.rule()
}

// BuildTestTicket assembles the body of the self-test ticket: the technical data
// of the printer and of the encoding in force, a column ruler, a sample of every
// character class a Spanish receipt uses, and a demonstration of the ESC/POS
// styles.
//
// The bytes returned are UTF-8 text plus formatting commands, NOT a finished
// payload: the caller hands them to rawPrint, which runs them through
// BuildESCPOSPayload with this printer's real options. That is the point — the
// ticket has to be encoded exactly like a sale receipt or it would be testing
// something nobody prints.
func BuildTestTicket(info PrinterTechnicalInfo, enc EncodingOptions, profile PrinterProfile, columns int, port int) []byte {
	if columns < testTicketMinColumns || columns > testTicketMaxColumns {
		columns = testTicketDefaultColumns
	}
	b := &ticketBuilder{columns: columns}

	cp, codePageActive, err := ResolveCodePage(enc.CodePage)
	if err != nil {
		cp = CodePage{Name: enc.CodePage, Description: "no soportada"}
	}

	// --- Cabecera -----------------------------------------------------------
	b.raw(escAlignCenter)
	b.raw(escSizeDouble)
	b.line("CRONOS POS")
	b.raw(escSizeNormal)
	b.raw(escBoldOn)
	b.line("TICKET DE PRUEBA")
	b.raw(escBoldOff)
	b.line("Agente v" + AgentVersion)
	b.line(time.Now().Format("02/01/2006 15:04:05"))
	b.raw(escAlignLeft)
	b.blank()
	b.doubleRule()

	// --- Impresora ----------------------------------------------------------
	b.section("DATOS DE LA IMPRESORA")
	b.field("Impresora", info.Name)
	b.field("Controlador", info.Driver)
	b.field("Puerto", info.Port)
	b.field("Procesador", info.Processor)
	b.field("Predeterm.", boolLabel(info.IsDefault, "Sí", "No"))
	b.field("En cola", fmt.Sprintf("%d trabajo(s)", info.QueuedIDs))
	for _, note := range info.Notes {
		b.field("Aviso", note)
	}

	// --- Agente -------------------------------------------------------------
	b.section("AGENTE")
	b.field("Versión", AgentVersion)
	b.field("Puerto HTTP", fmt.Sprintf("%d", port))
	b.field("Sistema", runtimeLabel())
	// Las dos preguntas que plantea una caja después de reiniciarse: ¿vuelve
	// solo el agente, y sabrá el operador encontrarlo si no vuelve?
	b.field("Inicio auto.", autostartStatusLabel())
	b.field("Acceso dir.", startMenuShortcutStatusLabel())

	// --- Codificación -------------------------------------------------------
	b.section("CODIFICACION")
	if !codePageActive {
		b.field("Página", "desactivada (none)")
	} else {
		b.field("Página", cp.Name+" — "+cp.Description)
		b.field("Comando", fmt.Sprintf("ESC t %d (1B 74 %02X)", enc.Selector(cp), enc.Selector(cp)))
	}
	b.field("Transcodif.", boolLabel(enc.Transcode, "Activada", "Desactivada"))
	b.field("Modo compat.", boolLabel(enc.Compatibility, "Activado", "Desactivado"))
	b.field("Plegado total", boolLabel(enc.StripAccents, "Activado", "Desactivado"))
	b.field("Reinicio", boolLabel(enc.Initialize, "ESC @ activado", "Desactivado"))

	if profile.CodePage == "" {
		b.field("Perfil", "sin calibrar")
	} else {
		b.field("Perfil", profile.CodePage)
		b.field("Origen", profileOriginLabel(profile))
	}

	// --- Regla de columnas --------------------------------------------------
	b.section("ANCHO DE LINEA")
	b.line(columnRuler(columns))
	b.line(columnScale(columns))
	b.field("Configurado", fmt.Sprintf("%d columnas", columns))

	// --- Juego de caracteres ------------------------------------------------
	b.section("ALFABETO")
	b.line("ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	b.line("abcdefghijklmnopqrstuvwxyz")

	b.section("NUMEROS")
	b.line("0123456789")
	b.line("1,234.56  $95.00  16%  #1000")

	b.section("ACENTOS Y ESPANOL")
	b.line("á é í ó ú   ü   ñ   ç")
	b.line("Á É Í Ó Ú   Ü   Ñ   Ç")
	b.line("à è ì ò ù   â ê î ô û")
	b.line("¿Cuánto? ¡Órale!")
	b.line("Morelia, Michoacán")
	b.line("Ánimo, ya falta menos")

	b.section("SIMBOLOS")
	b.paragraph("! \" # $ % & ' ( ) * + , - . / : ; < = > ? @ [ \\ ] ^ _ ` { | } ~")
	b.paragraph("º ª ° · ¬ ± ¼ ½ € £")

	// --- Estilos ------------------------------------------------------------
	b.section("ESTILOS ESC/POS")
	b.line("Texto normal")
	b.styled(escBoldOn, escBoldOff, "Texto en negrita")
	b.styled(escUnderlineOn, escUnderlineOff, "Texto subrayado")
	b.styled(escSizeWide, escSizeNormal, "Doble ancho")
	b.styled(escSizeTall, escSizeNormal, "Doble alto")
	b.styled(escSizeDouble, escSizeNormal, "Doble")
	b.styled(escAlignCenter, escAlignLeft, "Centrado")
	b.styled(escAlignRight, escAlignLeft, "Derecha")

	// --- Cómo leer el ticket ------------------------------------------------
	b.section("COMO LEER ESTE TICKET")
	if enc.Compatibility {
		b.paragraph("El modo compatible esta ACTIVO: las vocales mayusculas " +
			"acentuadas salen SIN acento a proposito, porque su byte depende de " +
			"la tabla que tenga cargada la impresora. Todo lo demas debe leerse " +
			"correctamente.")
		b.blank()
		b.paragraph("Para recuperarlas, calibre esta impresora con:")
	} else {
		b.paragraph("El modo compatible esta APAGADO: las vocales mayusculas " +
			"acentuadas viajan con su byte real. Si salen como simbolos de " +
			"dibujo, esta impresora no esta usando la pagina seleccionada y hay " +
			"que calibrarla con:")
	}
	b.line("POST /api/print/calibrate")
	b.blank()
	b.paragraph("Si alguna linea de acentos sale con simbolos de dibujo, " +
		"fotografie este ticket completo y enviela a soporte.")

	// --- Pie ----------------------------------------------------------------
	b.blank()
	b.doubleRule()
	b.raw(escAlignCenter)
	b.line("FIN DEL TICKET DE PRUEBA")
	b.raw(escAlignLeft)

	// Feed before the cut: the blade sits a few millimetres above the print
	// head, so cutting without advancing takes the last lines with it.
	b.text("\n\n\n\n")
	b.raw(escCutPaper)

	return b.out
}

// columnRuler draws the tens markers of the width ruler ("....5...10...15").
func columnRuler(columns int) string {
	var sb strings.Builder
	for i := 1; i <= columns; i++ {
		switch {
		case i%10 == 0:
			mark := fmt.Sprintf("%d", i)
			// The number is written ending at this column, so it has to back
			// up over the dots already emitted.
			current := sb.String()
			sb.Reset()
			sb.WriteString(current[:len(current)-len(mark)+1] + mark)
		case i%5 == 0:
			sb.WriteString("+")
		default:
			sb.WriteString(".")
		}
	}
	return sb.String()
}

// columnScale draws the repeating 1234567890 strip under the ruler, so the
// operator can count the exact column where the line wraps.
func columnScale(columns int) string {
	var sb strings.Builder
	for i := 1; i <= columns; i++ {
		sb.WriteString(fmt.Sprintf("%d", i%10))
	}
	return sb.String()
}

// boolLabel renders a flag with the words that make sense on a receipt, instead
// of the "true"/"false" that means nothing to the person reading it.
func boolLabel(value bool, yes, no string) string {
	if value {
		return yes
	}
	return no
}

// profileOriginLabel describes where a printer profile came from, in the words
// the ticket uses.
func profileOriginLabel(profile PrinterProfile) string {
	origin := "sin verificar"
	switch profile.Source {
	case profileSourceCalibration:
		origin = "calibrado"
	case profileSourceModel:
		origin = "por modelo"
	case profileSourceManual:
		origin = "manual"
	}
	if profile.Verified {
		return origin + ", verificado"
	}
	return origin
}

// runtimeLabel names the platform the agent is running on, for the technical
// block of the ticket: a support case that starts with the wrong operating
// system in mind wastes the first half of the call.
func runtimeLabel() string {
	return fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
}

// PrintTestTicket builds the self-test ticket for one printer and sends it
// through the ordinary print path.
//
// The encoding options are resolved exactly as they would be for a sale
// receipt (EncodingOptionsFor with this printer's name, so its profile
// applies), and rawPrint runs the body through BuildESCPOSPayload like any
// other ticket. A self-test that took a shortcut here would be testing a code
// path no customer receipt ever follows.
func PrintTestTicket(printerName string, columns int) error {
	if strings.TrimSpace(printerName) == "" {
		return fmt.Errorf("el nombre de la impresora es obligatorio")
	}

	enc := EncodingOptionsFor(printerName, "", nil)
	body := BuildTestTicket(
		describePrinter(printerName),
		enc,
		PrinterProfileFor(printerName),
		columns,
		int(agentPort.Load()),
	)

	return rawPrint(printerName, body, enc)
}
