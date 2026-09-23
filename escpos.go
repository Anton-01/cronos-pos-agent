package main

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/unicode/norm"
)

// Comandos ESC/POS relevantes para la codificación de caracteres.
var (
	// escInitialize es "ESC @" (0x1B 0x40): reinicia la impresora y, con ella,
	// la página de códigos activa. Si el payload empieza con este comando, la
	// selección de página debe inyectarse DESPUÉS o quedaría anulada.
	escInitialize = []byte{0x1B, 0x40}

	// escSelectCodeTable es "ESC t" (0x1B 0x74): selecciona la tabla de
	// caracteres. El byte siguiente (n) identifica la página de códigos.
	escSelectCodeTable = []byte{0x1B, 0x74}
)

// CodePage describe una página de códigos soportada por el hardware ESC/POS.
type CodePage struct {
	// Name es el identificador canónico usado en config.json y en la API.
	Name string
	// Selector es el valor "n" del comando "ESC t n" que activa la página.
	Selector byte
	// Charmap es la tabla de caracteres de golang.org/x/text que traduce el
	// texto UTF-8 a los bytes de esta página.
	Charmap *charmap.Charmap
	// Description es la etiqueta legible que se escribe en el log.
	Description string
}

// codePageNone desactiva por completo el tratamiento de codificación: el
// payload viaja al spooler byte por byte, tal cual lo envió el frontend.
const codePageNone = "none"

// defaultCodePage is the table a ticket is encoded against when nobody has
// configured anything, and since v1.9.0 it is PC858 again — no longer CP1252.
//
// The reasoning behind CP1252 (v1.5.0–v1.8.0) was that its bytes are Latin-1's,
// which is what a printer hanging off a Windows box is assumed to expect. The
// field disproved it: the agent writes RAW bytes straight to the spooler, so no
// Windows driver ever translates them, and a printer that ignores "ESC t 16"
// keeps decoding PC437. Under that failure —the common one— CP1252 is the worst
// of the four choices, because it shares no byte with PC437 above ASCII and
// every single accent comes out wrong ("Michoacßn"). PC858 shares its bytes with
// PC437 for all of them but four, so the same failure costs the accent on
// Á Í Ó Ú and nothing else.
//
// Those four are then handled by compatibility mode, which folds them to ASCII
// until the printer is verified (see escpos_compat.go). The pair —PC858 plus the
// fold— is the only configuration that cannot print garbage on hardware nobody
// has surveyed, which is the property a receipt printer in a shop needs.
const defaultCodePage = "cp858"

// codePageAuto is the name that spells out the pairing above in config.json:
// PC858 with compatibility mode left to its default (on until the printer is
// verified). It is an alias of "cp858" — the mode is a separate key — and it
// exists so that an operator reading the file can tell a value the agent chose
// from one a human deliberately pinned.
const codePageAuto = "auto"

// supportedCodePages indexa las páginas soportadas por su nombre canónico.
// Los valores de "n" siguen la tabla estándar de Epson ESC/POS, respetada por
// la práctica totalidad de ticketeras compatibles del mercado:
//
//	ESC t 16 (0x10) -> WPC1252    ESC t 2  (0x02) -> PC850
//	ESC t 19 (0x13) -> PC858      ESC t 0  (0x00) -> PC437
//
// Nótese que 0x13 es PC858 (CP850 + €) y NO CP1252: son páginas distintas
// aunque ambas cubran el español. Un modelo con una numeración propia se
// corrige sin recompilar con "escpos_code_page_id" en config.json.
var supportedCodePages = map[string]CodePage{
	"cp850":  {Name: "cp850", Selector: 0x02, Charmap: codePageCP850, Description: "PC850 Multilingual (Latin-1)"},
	"cp858":  {Name: "cp858", Selector: 0x13, Charmap: codePageCP858, Description: "PC858 Euro (Latin-1 + €)"},
	"cp1252": {Name: "cp1252", Selector: 0x10, Charmap: codePageCP1252, Description: "WPC1252 (Windows Latin-1)"},
	"cp437":  {Name: "cp437", Selector: 0x00, Charmap: codePageCP437, Description: "PC437 USA/Standard Europe"},
}

// codePageAliases acepta las formas en que un frontend puede nombrar una
// página de códigos, para no obligarlo a conocer el identificador exacto.
var codePageAliases = map[string]string{
	"850": "cp850", "pc850": "cp850", "ibm850": "cp850", "latin1": "cp850", "multilingual": "cp850",
	"858": "cp858", "pc858": "cp858", "ibm858": "cp858", "euro": "cp858",
	codePageAuto: "cp858", "automatico": "cp858", "automático": "cp858",
	"1252": "cp1252", "wpc1252": "cp1252", "windows1252": "cp1252", "windows-1252": "cp1252", "winlatin1": "cp1252",
	"437": "cp437", "pc437": "cp437", "ibm437": "cp437", "usa": "cp437",
	"off": codePageNone, "raw": codePageNone, "disabled": codePageNone, "ninguna": codePageNone,
}

// EncodingOptions describe cómo tratar el payload antes de mandarlo al spooler.
type EncodingOptions struct {
	// CodePage es el nombre canónico de la página de códigos a activar.
	// Vacío equivale a defaultCodePage; codePageNone desactiva el tratamiento.
	CodePage string
	// Transcode indica si el texto UTF-8 debe convertirse a los bytes de la
	// página de códigos. Si es false sólo se antepone el comando "ESC t n".
	Transcode bool
	// StripAccents folds the diacritics of the ticket text before it is
	// encoded, so that "Ánimo" reaches the printer as "Animo". It is the
	// fallback for the hardware that ignores the code page selection; turning
	// it off keeps the real accented characters and leaves them to the
	// transcoder, which is what a printer that honours "ESC t n" wants.
	StripAccents bool
	// Compatibility restricts the ticket to the characters that PC437, PC850 and
	// PC858 encode with the very same byte, folding the four Spanish characters
	// they disagree on (Á Í Ó Ú) to ASCII. It is what makes a ticket printable
	// on a printer whose active code page is unknown: see escpos_compat.go. It
	// is turned off by a verified printer profile, which is when the four
	// remaining characters can be printed for real.
	Compatibility bool
	// Initialize prepends "ESC @" (printer reset) to the payload, so that every
	// ticket starts from a known state and the code page selection that follows
	// it cannot be undone by leftover state from the previous job. A payload
	// that already opens with its own "ESC @" is left alone: no second reset is
	// injected.
	Initialize bool
	// SelectorOverride sustituye el "n" de "ESC t n" por el valor indicado,
	// manteniendo la tabla de transcodificación de CodePage. Es la válvula de
	// escape para las ticketeras clónicas que numeran sus páginas de códigos
	// de otra forma que el estándar de Epson: se ajusta en config.json sin
	// recompilar el agente. nil = numeración estándar.
	SelectorOverride *byte
}

// DefaultEncodingOptions returns the treatment applied to a ticket when the
// agent has nothing configured: the default code page, transcoding on, accent
// folding on and the printer reset on. Every caller that builds EncodingOptions
// starts from here instead of from the zero value, so that a field added later
// does not silently default to false in one of the call sites.
func DefaultEncodingOptions() EncodingOptions {
	return EncodingOptions{
		CodePage:      defaultCodePage,
		Transcode:     true,
		StripAccents:  false,
		Compatibility: true,
		Initialize:    true,
	}
}

// Selector devuelve el byte "n" que se enviará en "ESC t n" para esta página,
// respetando el override de configuración si lo hay.
func (o EncodingOptions) Selector(cp CodePage) byte {
	if o.SelectorOverride != nil {
		return *o.SelectorOverride
	}
	return cp.Selector
}

// ResolveCodePage normaliza el nombre recibido (config o petición) y devuelve
// la página correspondiente. El segundo valor es false cuando el tratamiento
// debe desactivarse; el error se reserva para nombres desconocidos.
func ResolveCodePage(name string) (CodePage, bool, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.NewReplacer(" ", "", "_", "").Replace(normalized)

	if normalized == "" {
		normalized = defaultCodePage
	}
	if canonical, ok := codePageAliases[normalized]; ok {
		normalized = canonical
	}
	if normalized == codePageNone {
		return CodePage{Name: codePageNone}, false, nil
	}

	cp, ok := supportedCodePages[normalized]
	if !ok {
		return CodePage{}, false, fmt.Errorf("página de códigos '%s' no soportada (válidas: %s)", name, strings.Join(SupportedCodePageNames(), ", "))
	}
	return cp, true, nil
}

// SupportedCodePageNames lista los nombres canónicos aceptados, ordenados,
// para poder devolverlos en los mensajes de error de la API.
func SupportedCodePageNames() []string {
	names := make([]string, 0, len(supportedCodePages)+1)
	for name := range supportedCodePages {
		names = append(names, name)
	}
	sort.Strings(names)
	return append(names, codePageNone)
}

// BuildESCPOSPayload builds the definitive bytes written to the thermal printer:
//
//  1. Folds every diacritic of the ticket text, so that "Ánimo" travels as
//     "Animo" and "Michoacán" as "Michoacan" (see sanitizeTextForPrinter). This
//     is the blunt fallback kept for the hardware that prints nothing but plain
//     ASCII correctly; it is off by default and enabled with "strip_accents".
//  2. Folds only what the printer could get wrong, when compatibility mode is
//     on: the characters PC437, PC850 and PC858 encode identically are kept as
//     they are, and the four Spanish ones they disagree on (Á Í Ó Ú) are folded
//     to ASCII. "Michoacán" keeps its accent, "Ánimo" becomes "Animo", and the
//     ticket cannot print garbage on a printer whose active table is unknown
//     (see escpos_compat.go). The mode also pins the encoding to PC858, the page
//     whose bytes that subset is defined against.
//  3. Transcodes the UTF-8 text to the bytes of the printer's code page
//     (¿ ¡ € º …), which would otherwise print as pairs of garbage characters
//     because the printer decodes every UTF-8 byte on its own.
//  4. Prepends "ESC @" (0x1B 0x40) to reset the printer, unless the payload
//     already opens with one of its own or opts.Initialize is false.
//  5. Prepends "ESC t n" to select that same code page on the printer, always
//     behind the "ESC @" of step 4 — the reset restores the factory code page,
//     so a selection placed before it would be undone — and re-sends it after
//     every further "ESC @" the payload carries, for the same reason.
//
// If the payload already carries its own "ESC t", the emitter is assumed to
// manage the encoding itself and the bytes are returned untouched.
func BuildESCPOSPayload(data []byte, opts EncodingOptions) ([]byte, error) {
	cp, active, err := ResolveCodePage(opts.CodePage)
	if err != nil {
		return nil, err
	}
	if !active || len(data) == 0 {
		return data, nil
	}
	if hasCodePageCommand(data) {
		return data, nil
	}

	// Compatibility mode is defined against the bytes of the DOS family, so it
	// encodes with PC858 whatever the configuration named. Honouring a CP1252
	// setting here would defeat the mode: its safe subset does not survive a
	// Latin-1 layout, and the fold would be paying for a guarantee it no longer
	// provides. A deliberate CP1252 install turns the mode off instead — with
	// "escpos_compatibility": false or by verifying the printer.
	if opts.Compatibility && cp.Name != compatibilityCodePage {
		if forced, _, err := ResolveCodePage(compatibilityCodePage); err == nil {
			cp = forced
		}
	}

	// Diacritics go first, because folding an accent into its base letter turns
	// it into plain ASCII, and plain ASCII is the one thing every code page
	// —selected or ignored— prints the same way. Whatever survives the fold
	// (¿ ¡ € º) is still worth transcoding, so both layers stay.
	payload := data
	if opts.StripAccents {
		payload = sanitizePayloadText(payload)
	} else if opts.Compatibility {
		payload = foldPayloadToUniversalSafe(payload)
	}
	if opts.Transcode {
		payload = transcodeToCodePage(payload, cp.Charmap)
	}

	selector := opts.Selector(cp)
	return reassertAfterResets(insertEncodingPreamble(payload, selector, opts.Initialize), selector), nil
}

// sanitizeTextForPrinter removes the diacritical marks of a string and keeps the
// base letter: "Ánimo" becomes "Animo", "ARTÍCULO ÑOÑO" becomes "ARTICULO NONO".
//
// What it solves. Selecting a code page with "ESC t n" is the correct fix on
// paper, but part of the hardware in the field ignores that command outright and
// keeps decoding every byte against whatever table its firmware booted with. On
// those printers no encoding of "Á" is right —the byte lands on a different
// glyph in every table— so the only text that prints reliably is text that has
// no accented characters left in it. Folding the accent away loses a diacritic;
// not folding it prints "†nimo".
//
// How it works. The string is first converted to NFD (Normalization Form
// Canonical Decomposition), which splits every precomposed character into its
// base letter plus its combining marks: "Á" (U+00C1) becomes "A" (U+0041)
// followed by U+0301 COMBINING ACUTE ACCENT. The decomposed runes are then
// walked one by one and every combining mark is discarded — those are the runes
// in Unicode category Mn ("Mark, nonspacing"), tested with
// unicode.Is(unicode.Mn, r). What is left is the bare base letter, already in
// its composed form because a lone ASCII letter needs no recomposition.
//
// Note that the fold also reaches the tilde of "Ñ", which NFD decomposes exactly
// like an accent ("N" + U+0303): "Niño" prints as "Nino". That is inherent to
// the fallback — a printer that ignores the code page cannot render "Ñ" either.
//
// Characters with no canonical decomposition (¿ ¡ € ß ø …) are not marks and
// pass through untouched; they are handled downstream by the code page encoder
// and, failing that, by asciiFallback.
func sanitizeTextForPrinter(input string) string {
	decomposed := norm.NFD.String(input)

	var out strings.Builder
	out.Grow(len(decomposed))
	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) {
			continue // combining mark: the accent itself, dropped
		}
		out.WriteRune(r)
	}
	return out.String()
}

// sanitizePayloadText applies sanitizeTextForPrinter to every stretch of text in
// a RAW ESC/POS payload, leaving commands and binary data alone. It walks the
// payload with the same conservative reader the transcoder uses (mapTextRuns),
// because a ticket is not a string: running the whole buffer through the
// normalizer would corrupt the raster bytes of a logo.
func sanitizePayloadText(data []byte) []byte {
	return mapTextRuns(data, func(run []byte) []byte {
		return []byte(sanitizeTextForPrinter(string(run)))
	})
}

// codePagePreambleScan limita la búsqueda de un "ESC t" propio a la cabecera del
// payload. Buscarlo en todo el ticket daría falsos positivos dentro de los datos
// binarios de un logo (dos bytes cualesquiera pueden valer 0x1B 0x74) y el
// agente dejaría de corregir los acentos sin motivo aparente.
const codePagePreambleScan = 64

// hasCodePageCommand indica si el payload ya trae una selección de página de
// códigos propia ("ESC t n") en su cabecera, en cuyo caso se asume que el emisor
// gestiona la codificación y el agente no debe interferir.
func hasCodePageCommand(data []byte) bool {
	limit := len(data)
	if limit > codePagePreambleScan {
		limit = codePagePreambleScan
	}
	for i := 0; i+2 < limit; i++ {
		if data[i] == escSelectCodeTable[0] && data[i+1] == escSelectCodeTable[1] {
			return true
		}
	}
	return false
}

// insertEncodingPreamble writes the two commands that every ticket must open
// with —"ESC @" (reset) and "ESC t n" (code page selection)— at the head of the
// payload, in that order.
//
// The order is not negotiable: "ESC @" restores the factory code page, so a
// selection placed before it would be wiped out and the ticket would print
// garbage again. That is also why the selection is injected BEHIND the leading
// "ESC @" bytes the payload may already carry, instead of at offset 0.
//
// The reset is only added when the payload does not already open with one:
// sending it twice would be harmless on paper, but a second reset in the middle
// of what the frontend considers its own preamble is the kind of surprise that
// is impossible to debug from a receipt. Pass initialize=false to leave the
// reset out altogether (config key "escpos_initialize").
func insertEncodingPreamble(data []byte, selector byte, initialize bool) []byte {
	offset := 0
	for offset+1 < len(data) && data[offset] == escInitialize[0] && data[offset+1] == escInitialize[1] {
		offset += 2
	}

	command := make([]byte, 0, len(escInitialize)+len(escSelectCodeTable)+1)
	if initialize && offset == 0 {
		command = append(command, escInitialize...)
	}
	command = append(command, escSelectCodeTable[0], escSelectCodeTable[1], selector)

	out := make([]byte, 0, len(data)+len(command))
	out = append(out, data[:offset]...)
	out = append(out, command...)
	out = append(out, data[offset:]...)
	return out
}

// reassertAfterResets re-sends "ESC t n" behind every "ESC @" that appears
// further down the payload, so that a reset the frontend emits in the middle of
// a ticket cannot silently undo the code page selection made at the top.
//
// "ESC @" restores the printer to its power-on state, and that includes the
// character table: everything printed after it decodes against the factory page
// —PC437 on nearly all the hardware— no matter what was selected before. A
// frontend that opens each logical section of a ticket with its own reset (a
// common way to make sure the logo, the body and the footer all start from a
// known state) therefore prints the first section with accents and the rest
// without them, which is the kind of defect that is impossible to read off a
// receipt. Three bytes after each reset removes the whole failure mode.
//
// A reset that already carries its own selection is left alone, which is what
// keeps this from duplicating the preamble insertEncodingPreamble just wrote.
// Graphics blocks are skipped with graphicsCommandLength, because two bytes of a
// logo's raster data can perfectly well read 0x1B 0x40 and injecting a command
// into the middle of an image would corrupt the rest of the ticket.
func reassertAfterResets(data []byte, selector byte) []byte {
	out := make([]byte, 0, len(data))

	for i := 0; i < len(data); {
		if n := graphicsCommandLength(data[i:]); n > 0 {
			out = append(out, data[i:i+n]...)
			i += n
			continue
		}

		if i+1 < len(data) && data[i] == escInitialize[0] && data[i+1] == escInitialize[1] {
			out = append(out, escInitialize...)
			i += 2
			// A selection is pointless in front of another reset, which would
			// wipe it two bytes later: a payload opening with several "ESC @"
			// in a row gets one selection, behind the last of them.
			if !startsWithCodePageCommand(data[i:]) && !startsWithInitialize(data[i:]) {
				out = append(out, escSelectCodeTable[0], escSelectCodeTable[1], selector)
			}
			continue
		}

		out = append(out, data[i])
		i++
	}

	return out
}

// startsWithCodePageCommand reports whether data opens with "ESC t n", the
// selection reassertAfterResets would otherwise add.
func startsWithCodePageCommand(data []byte) bool {
	return len(data) >= 3 && data[0] == escSelectCodeTable[0] && data[1] == escSelectCodeTable[1]
}

// startsWithInitialize reports whether data opens with "ESC @".
func startsWithInitialize(data []byte) bool {
	return len(data) >= 2 && data[0] == escInitialize[0] && data[1] == escInitialize[1]
}

// transcodeToCodePage convierte las secuencias UTF-8 del payload a los bytes de
// la página de códigos indicada, usando el codificador de
// golang.org/x/text/encoding/charmap:
//
//	encoder := charmap.CodePage858.NewEncoder()
//	bytes, err := encoder.Bytes(textoDelTicket)   // "ñ" -> 0xA4
//
// El recorrido es conservador a propósito, porque un ticket RAW **no es una
// cadena de texto**: mezcla texto con comandos y con datos binarios (logos,
// imágenes raster). Pasar el payload entero por el codificador corrompería el
// ticket, así que sólo se codifican los tramos que de verdad son texto:
//
//   - Los comandos gráficos se detectan por su cabecera y sus datos binarios se
//     copian en bloque sin interpretarlos (ver graphicsCommandLength).
//   - Los bytes ASCII (< 0x80) se copian sin tocar: son idénticos en las cuatro
//     páginas y cubren todos los comandos ESC/POS.
//   - Los tramos de runas UTF-8 válidas se codifican de una vez con el encoder.
//   - Los bytes sueltos que no forman UTF-8 válido se copian tal cual: son
//     binarios, no texto.
func transcodeToCodePage(data []byte, cm *charmap.Charmap) []byte {
	// El codificador se crea por llamada y no se comparte: es un transformador
	// con estado interno y rawPrint puede ejecutarse en paralelo desde varias
	// peticiones HTTP.
	encoder := cm.NewEncoder()

	return mapTextRuns(data, func(run []byte) []byte {
		return encodeTextRun(encoder, cm, run)
	})
}

// mapTextRuns walks a RAW ESC/POS payload, hands every stretch of real text to
// transform and copies everything else —commands, ASCII, binary data— verbatim.
// It is the shared reader behind the two transformations the agent applies to a
// ticket: the diacritic fold (sanitizePayloadText) and the code page
// transcoding (transcodeToCodePage). Both need exactly the same notion of
// "which bytes of this buffer are text", and getting that wrong corrupts logos,
// so the rule lives in one place.
func mapTextRuns(data []byte, transform func(run []byte) []byte) []byte {
	out := make([]byte, 0, len(data))

	for i := 0; i < len(data); {
		if n := graphicsCommandLength(data[i:]); n > 0 {
			out = append(out, data[i:i+n]...)
			i += n
			continue
		}

		b := data[i]
		if b < utf8.RuneSelf {
			out = append(out, b)
			i++
			continue
		}

		r, size := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && size <= 1 {
			// Byte suelto no decodificable: es binario, se respeta.
			out = append(out, b)
			i++
			continue
		}

		// Tramo de texto: se acumulan las runas válidas consecutivas y se
		// codifican en una sola pasada. Un comando ESC/POS nunca empieza por un
		// byte ≥ 0x80, así que el tramo no puede tragarse una cabecera.
		start := i
		i += size
		for i < len(data) && data[i] >= utf8.RuneSelf {
			r, size := utf8.DecodeRune(data[i:])
			if r == utf8.RuneError && size <= 1 {
				break
			}
			i += size
		}

		out = append(out, transform(data[start:i])...)
	}

	return out
}

// encodeTextRun codifica un tramo de texto UTF-8 a la página de códigos activa.
//
// El camino rápido es una sola llamada al encoder de charmap. Si el tramo
// contiene alguna runa que la página no representa, el encoder devuelve error y
// entonces —y sólo entonces— se recorre runa a runa para degradarla a su
// equivalente ASCII (`asciiFallback`: `Á`→`A`, `€`→`EUR`, `…`→`...`) y, como
// último recurso, a '?'. Nunca se imprime basura ni se aborta el ticket por un
// carácter exótico.
func encodeTextRun(encoder *encoding.Encoder, cm *charmap.Charmap, run []byte) []byte {
	if encoded, err := encoder.Bytes(run); err == nil {
		return encoded
	}

	out := make([]byte, 0, len(run))
	for _, r := range string(run) {
		if b, ok := cm.EncodeRune(r); ok {
			out = append(out, b)
			continue
		}
		if fallback, ok := asciiFallback[r]; ok {
			out = append(out, fallback...)
			continue
		}
		out = append(out, '?')
	}
	return out
}

// graphicsCommandLength reconoce los comandos ESC/POS que llevan datos binarios
// detrás de su cabecera y devuelve el tamaño total del bloque (cabecera + datos)
// que debe copiarse literalmente. Devuelve 0 si data no empieza por uno de ellos.
//
// Sin esta detección, los bytes de un logo que por casualidad formen una
// secuencia UTF-8 válida se traducirían a un solo byte: la imagen se corrompería
// y, al descuadrarse la longitud declarada, la impresora interpretaría el resto
// del ticket como comandos.
//
// Si la longitud declarada excede el buffer se devuelve todo lo que queda: ante
// un payload truncado, copiar de más es siempre más seguro que transcodificar.
func graphicsCommandLength(data []byte) int {
	clamp := func(total int) int {
		if total > len(data) {
			return len(data)
		}
		return total
	}
	le16 := func(lo, hi byte) int { return int(lo) | int(hi)<<8 }

	switch {
	// ESC * m nL nH  — modo bit image
	case len(data) >= 5 && data[0] == 0x1B && data[1] == 0x2A:
		dots := le16(data[3], data[4])
		if data[2] == 32 || data[2] == 33 {
			dots *= 3
		}
		return clamp(5 + dots)

	// GS v 0 m xL xH yL yH  — raster bit image (el más usado para logos).
	// El tercer byte forma parte del nombre del comando: 0x30 según la
	// especificación de Epson, 0x00 en los clones que también lo aceptan.
	case len(data) >= 8 && data[0] == 0x1D && data[1] == 0x76 && (data[2] == 0x30 || data[2] == 0x00):
		return clamp(8 + le16(data[4], data[5])*le16(data[6], data[7]))

	// GS * x y  — definición de imagen en RAM
	case len(data) >= 4 && data[0] == 0x1D && data[1] == 0x2A:
		return clamp(4 + int(data[2])*int(data[3])*8)

	// GS ( fn pL pH  — familia de comandos con longitud explícita (gráficos,
	// códigos QR/2D, etc.)
	case len(data) >= 5 && data[0] == 0x1D && data[1] == 0x28:
		return clamp(5 + le16(data[3], data[4]))

	// GS 8 L p1 p2 p3 p4  — gráficos grandes con longitud de 32 bits
	case len(data) >= 7 && data[0] == 0x1D && data[1] == 0x38 && data[2] == 0x4C:
		size := int(data[3]) | int(data[4])<<8 | int(data[5])<<16 | int(data[6])<<24
		return clamp(7 + size)
	}

	return 0
}
