package main

import (
	"bytes"
	"testing"
)

// Bytes de referencia de las vocales acentuadas mayúsculas en CP850, que es el
// caso que fallaba en producción: la ticketera recibía UTF-8 crudo (dos bytes
// por vocal) e imprimía dos símbolos ilegibles en su lugar.
func TestTranscodeUppercaseAccents(t *testing.T) {
	cases := []struct {
		text string
		want []byte
	}{
		{"Á", []byte{0xB5}},
		{"É", []byte{0x90}},
		{"Í", []byte{0xD6}},
		{"Ó", []byte{0xE0}},
		{"Ú", []byte{0xE9}},
		{"Ñ", []byte{0xA5}},
		{"Ü", []byte{0x9A}},
		{"á", []byte{0xA0}},
		{"ñ", []byte{0xA4}},
		{"¿", []byte{0xA8}},
		{"¡", []byte{0xAD}},
		{"ARTÍCULO ÑOÑO", []byte("ART\xD6CULO \xA5O\xA5O")},
	}

	for _, tc := range cases {
		got := transcodeToCodePage([]byte(tc.text), codePageCP850)
		if !bytes.Equal(got, tc.want) {
			t.Errorf("transcodeToCodePage(%q) = % X, se esperaba % X", tc.text, got, tc.want)
		}
	}
}

func TestTranscodeCP1252(t *testing.T) {
	// En CP1252 las mayúsculas acentuadas coinciden con Latin-1.
	got := transcodeToCodePage([]byte("ÁÉÍÓÚÑ"), codePageCP1252)
	want := []byte{0xC1, 0xC9, 0xCD, 0xD3, 0xDA, 0xD1}
	if !bytes.Equal(got, want) {
		t.Errorf("CP1252: % X, se esperaba % X", got, want)
	}
}

// El caso exacto que fallaba en la caja de cobro: "Ánimo" salía impreso como
// "†nimo". El ticket abre con ESC t 16 (CP1252) y la Á llega ya plegada a una
// "A" ASCII, que es lo único que imprime igual en toda tabla de caracteres,
// tenga o no el hardware en cuenta la selección de página.
func TestBuildPayloadFoldsAccentsOfTheAnimoCase(t *testing.T) {
	got, err := BuildESCPOSPayload([]byte("Ánimo"), DefaultEncodingOptions())
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	want := []byte{0x1B, 0x40, 0x1B, 0x74, 0x10, 'A', 'n', 'i', 'm', 'o'}
	if !bytes.Equal(got, want) {
		t.Errorf("payload = % X, se esperaba % X", got, want)
	}
}

// sanitizeTextForPrinter descompone a NFD y descarta las marcas (categoría Mn),
// dejando la letra base. La eñe entra en el mismo saco: su virgulilla es una
// marca combinante, así que "Niño" se imprime "Nino".
func TestSanitizeTextForPrinter(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Ánimo", "Animo"},
		{"ARTÍCULO ÑOÑO", "ARTICULO NONO"},
		{"Café con leche", "Cafe con leche"},
		{"Niño", "Nino"},
		{"ÁÉÍÓÚáéíóúÜü", "AEIOUaeiouUu"},
		{"TOTAL: 25.00", "TOTAL: 25.00"}, // ASCII puro: intacto
		{"", ""},
		// Ya descompuesto en la entrada: "A" + U+0301. La base es ASCII y se
		// copia antes; la marca llega suelta y también debe desaparecer.
		{"Ánimo", "Animo"},
		// Sin descomposición canónica y sin ser marcas: no son asunto de esta
		// función, los resuelve después el codificador de la página de códigos.
		{"¿Cuánto? 25 €", "¿Cuanto? 25 €"},
	}

	for _, tc := range cases {
		if got := sanitizeTextForPrinter(tc.input); got != tc.want {
			t.Errorf("sanitizeTextForPrinter(%q) = %q, se esperaba %q", tc.input, got, tc.want)
		}
	}
}

// La limpieza recorre el payload con el mismo lector conservador que el
// transcodificador: un logo raster lleva bytes que por casualidad forman UTF-8
// válido y normalizarlos corrompería la imagen.
func TestSanitizePayloadTextSkipsRasterImageData(t *testing.T) {
	// 0xC3 0x81 es "Á" en UTF-8, incrustado aquí dentro de los datos de imagen.
	image := []byte{0xC3, 0x81, 0xC2, 0xB0, 0xFF, 0x00}
	input := append([]byte{0x1D, 0x76, 0x30, 0x00, 0x02, 0x00, 0x03, 0x00}, image...)
	input = append(input, []byte("Ánimo")...)

	got := sanitizePayloadText(input)

	want := append([]byte{0x1D, 0x76, 0x30, 0x00, 0x02, 0x00, 0x03, 0x00}, image...)
	want = append(want, []byte("Animo")...)

	if !bytes.Equal(got, want) {
		t.Errorf("raster alterado:\n got % X\nwant % X", got, want)
	}
}

// Los datos binarios sueltos (los que no forman UTF-8 válido) atraviesan la
// limpieza byte a byte.
func TestSanitizePayloadTextLeavesBinaryUntouched(t *testing.T) {
	input := []byte{0x1B, 0x40, 0xFF, 0x81, 0xC0, 0xFE, 0xAA, 0x0A}
	if got := sanitizePayloadText(input); !bytes.Equal(got, input) {
		t.Errorf("los datos binarios se alteraron:\n got % X\nwant % X", got, input)
	}
}

// Las ticketeras clónicas que numeran sus páginas de códigos de otra forma se
// corrigen desde config.json con escpos_code_page_id, sin recompilar: el "n"
// del comando cambia pero la transcodificación sigue siendo la de CP1252.
func TestBuildPayloadSelectorOverride(t *testing.T) {
	selector := byte(0x13)
	opts := DefaultEncodingOptions()
	opts.CodePage = "cp1252"
	opts.SelectorOverride = &selector

	got, err := BuildESCPOSPayload([]byte("¿Á?"), opts)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	// La "Á" se pliega a "A" antes de codificar; la "¿" no lleva marca alguna,
	// así que sigue el camino de siempre y viaja como 0xBF (CP1252).
	want := []byte{0x1B, 0x40, 0x1B, 0x74, 0x13, 0xBF, 'A', '?'}
	if !bytes.Equal(got, want) {
		t.Errorf("payload = % X, se esperaba % X", got, want)
	}
}

// CP437 (la página de fábrica de la mayoría de ticketeras) sólo contiene É de
// las cinco vocales acentuadas mayúsculas — de ahí el fallo de producción. Las
// que no existen deben degradarse a su equivalente ASCII en vez de imprimirse
// como basura; por eso el valor por defecto del agente es CP1252 y no CP437.
func TestTranscodeFallbackWhenCodePageLacksRune(t *testing.T) {
	got := transcodeToCodePage([]byte("ÁÉÍÓÚ ñ"), codePageCP437)
	want := []byte("A\x90IOU \xA4")
	if !bytes.Equal(got, want) {
		t.Errorf("CP437: % X, se esperaba % X", got, want)
	}
}

// El codificador de charmap devuelve error si el tramo contiene una runa que la
// página no representa. Ese error no debe abortar el ticket: se recorre el tramo
// runa a runa y se degrada sólo la que sobra.
func TestTranscodeFallbackWithinTextRun(t *testing.T) {
	// 'é' sí está en CP1252 (0xE9); 'Ω' no, y tampoco tiene equivalente ASCII.
	got := transcodeToCodePage([]byte("Café Ω y 25 €"), codePageCP1252)
	want := []byte{'C', 'a', 'f', 0xE9, ' ', '?', ' ', 'y', ' ', '2', '5', ' ', 0x80}

	if !bytes.Equal(got, want) {
		t.Errorf("payload = % X, se esperaba % X", got, want)
	}
}

func TestTranscodePreservesASCIIAndCommands(t *testing.T) {
	// Comandos ESC/POS típicos: ESC @ (init), ESC a 1 (centrar), GS V 0 (corte).
	input := []byte{0x1B, 0x40, 0x1B, 0x61, 0x01, 'T', 'O', 'T', 'A', 'L', 0x0A, 0x1D, 0x56, 0x00}
	got := transcodeToCodePage(input, codePageCP850)
	if !bytes.Equal(got, input) {
		t.Errorf("los comandos ASCII se alteraron: % X", got)
	}
}

// Los datos binarios (logos raster, códigos de barras) no son UTF-8 válido y
// deben atravesar el transcodificador intactos byte a byte.
func TestTranscodeLeavesBinaryUntouched(t *testing.T) {
	input := []byte{0x1D, 0x76, 0x30, 0x00, 0x04, 0x00, 0x08, 0x00, 0xFF, 0x81, 0xC0, 0xFE, 0xAA}
	got := transcodeToCodePage(input, codePageCP850)
	if !bytes.Equal(got, input) {
		t.Errorf("los datos binarios se alteraron:\n got % X\nwant % X", got, input)
	}
}

// Un logo raster contiene bytes arbitrarios; algunos pares forman por casualidad
// secuencias UTF-8 válidas. Si el transcodificador los tradujera, la imagen se
// corrompería y la impresora leería el resto del ticket como comandos.
func TestTranscodeSkipsRasterImageData(t *testing.T) {
	// GS v 0 m xL xH yL yH + 2*3 bytes de datos, con 0xC3 0xA9 ("é" en UTF-8)
	// y 0xC2 0xB0 ("°") incrustados en la imagen.
	image := []byte{0xC3, 0xA9, 0xC2, 0xB0, 0xFF, 0x00}
	input := append([]byte{0x1D, 0x76, 0x30, 0x00, 0x02, 0x00, 0x03, 0x00}, image...)
	input = append(input, []byte("Café")...)

	got := transcodeToCodePage(input, codePageCP850)

	want := append([]byte{0x1D, 0x76, 0x30, 0x00, 0x02, 0x00, 0x03, 0x00}, image...)
	want = append(want, 'C', 'a', 'f', 0x82)

	if !bytes.Equal(got, want) {
		t.Errorf("raster alterado:\n got % X\nwant % X", got, want)
	}
}

func TestGraphicsCommandLength(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want int
	}{
		{"GS v 0 raster 2x3", append([]byte{0x1D, 0x76, 0x30, 0x00, 0x02, 0x00, 0x03, 0x00}, make([]byte, 6)...), 14},
		{"ESC * 8 puntos", append([]byte{0x1B, 0x2A, 0x00, 0x04, 0x00}, make([]byte, 4)...), 9},
		{"ESC * 24 puntos", append([]byte{0x1B, 0x2A, 0x21, 0x04, 0x00}, make([]byte, 12)...), 17},
		{"GS * 2x1", append([]byte{0x1D, 0x2A, 0x02, 0x01}, make([]byte, 16)...), 20},
		{"GS ( k QR", append([]byte{0x1D, 0x28, 0x6B, 0x03, 0x00}, make([]byte, 3)...), 8},
		{"GS 8 L", append([]byte{0x1D, 0x38, 0x4C, 0x05, 0x00, 0x00, 0x00}, make([]byte, 5)...), 12},
		{"longitud declarada mayor que el buffer", []byte{0x1D, 0x76, 0x30, 0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0x01}, 9},
		{"texto normal", []byte("TOTAL"), 0},
		{"corte de papel GS V", []byte{0x1D, 0x56, 0x00}, 0},
		{"negrita ESC E", []byte{0x1B, 0x45, 0x01}, 0},
	}

	for _, tc := range cases {
		if got := graphicsCommandLength(tc.data); got != tc.want {
			t.Errorf("%s: graphicsCommandLength = %d, se esperaba %d", tc.name, got, tc.want)
		}
	}
}

func TestBuildPayloadPrependsCodePageCommand(t *testing.T) {
	opts := DefaultEncodingOptions()
	opts.CodePage = "cp850"

	got, err := BuildESCPOSPayload([]byte("TOTAL"), opts)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	want := append([]byte{0x1B, 0x40, 0x1B, 0x74, 0x02}, []byte("TOTAL")...)
	if !bytes.Equal(got, want) {
		t.Errorf("payload = % X, se esperaba % X", got, want)
	}
}

// "ESC @" reinicia la impresora y restaura la página de códigos de fábrica: si
// la selección se antepusiera a él quedaría anulada y el ticket volvería a
// imprimir basura.
func TestBuildPayloadInsertsAfterInitialize(t *testing.T) {
	input := append([]byte{0x1B, 0x40}, []byte("Café")...)

	opts := DefaultEncodingOptions()
	opts.CodePage = "cp858"

	got, err := BuildESCPOSPayload(input, opts)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	// Un solo "ESC @": el del propio payload. El agente no añade el suyo.
	want := []byte{0x1B, 0x40, 0x1B, 0x74, 0x13, 'C', 'a', 'f', 'e'}
	if !bytes.Equal(got, want) {
		t.Errorf("payload = % X, se esperaba % X", got, want)
	}
}

func TestBuildPayloadRespectsExistingCodePageCommand(t *testing.T) {
	// El frontend ya seleccionó su página: el agente no debe interferir.
	input := []byte{0x1B, 0x74, 0x10, 'A', 0xC1}

	opts := DefaultEncodingOptions()
	opts.CodePage = "cp850"

	got, err := BuildESCPOSPayload(input, opts)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if !bytes.Equal(got, input) {
		t.Errorf("payload = % X, se esperaba intacto % X", got, input)
	}
}

// Con escpos_transcode desactivado no hay traducción a la página de códigos,
// pero el pliegue de acentos sí se aplica: es la única capa que sigue en pie
// cuando el hardware ignora la selección de página, así que no depende de esa
// preferencia. La "¿", que no lleva marca, viaja en su UTF-8 original.
func TestBuildPayloadWithoutTranscoding(t *testing.T) {
	input := []byte("Á¿")

	opts := DefaultEncodingOptions()
	opts.CodePage = "cp850"
	opts.Transcode = false

	got, err := BuildESCPOSPayload(input, opts)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	want := append([]byte{0x1B, 0x40, 0x1B, 0x74, 0x02, 'A'}, []byte("¿")...)
	if !bytes.Equal(got, want) {
		t.Errorf("payload = % X, se esperaba % X", got, want)
	}
}

func TestBuildPayloadCodePageNoneIsPassthrough(t *testing.T) {
	input := []byte("Ó")

	opts := DefaultEncodingOptions()
	opts.CodePage = codePageNone

	got, err := BuildESCPOSPayload(input, opts)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if !bytes.Equal(got, input) {
		t.Errorf("payload = % X, se esperaba intacto % X", got, input)
	}
}

func TestResolveCodePage(t *testing.T) {
	cases := []struct {
		name         string
		wantName     string
		wantSelector byte
		wantActive   bool
	}{
		{"", "cp1252", 0x10, true}, // sin nombre -> defaultCodePage (CP1252)
		{"CP850", "cp850", 0x02, true},
		{" 850 ", "cp850", 0x02, true},
		{"cp858", "cp858", 0x13, true},
		{"Windows-1252", "cp1252", 0x10, true},
		{"pc437", "cp437", 0x00, true},
		{"none", codePageNone, 0x00, false},
	}

	for _, tc := range cases {
		cp, active, err := ResolveCodePage(tc.name)
		if err != nil {
			t.Errorf("ResolveCodePage(%q) devolvió error: %v", tc.name, err)
			continue
		}
		if active != tc.wantActive {
			t.Errorf("ResolveCodePage(%q) activo = %v, se esperaba %v", tc.name, active, tc.wantActive)
		}
		if cp.Name != tc.wantName {
			t.Errorf("ResolveCodePage(%q) nombre = %q, se esperaba %q", tc.name, cp.Name, tc.wantName)
		}
		if active && cp.Selector != tc.wantSelector {
			t.Errorf("ResolveCodePage(%q) selector = 0x%02X, se esperaba 0x%02X", tc.name, cp.Selector, tc.wantSelector)
		}
	}

	if _, _, err := ResolveCodePage("cp932"); err == nil {
		t.Error("ResolveCodePage(\"cp932\") debería fallar: página no soportada")
	}
}

func TestBuildPayloadEmptyInput(t *testing.T) {
	opts := DefaultEncodingOptions()
	opts.CodePage = "cp850"

	got, err := BuildESCPOSPayload(nil, opts)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("un payload vacío no debe generar comandos: % X", got)
	}
}

// Con la configuración por defecto todo ticket abre con la pareja completa:
// "ESC @" (reinicio) y "ESC t 16" (CP1252), en ese orden. Al revés el reinicio
// borraría la selección de página y el ticket volvería a imprimir basura.
func TestBuildPayloadEmitsInitializeAndCodePage(t *testing.T) {
	got, err := BuildESCPOSPayload([]byte("TOTAL"), DefaultEncodingOptions())
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	want := append([]byte{0x1B, 0x40, 0x1B, 0x74, 0x10}, []byte("TOTAL")...)
	if !bytes.Equal(got, want) {
		t.Errorf("payload = % X, se esperaba % X", got, want)
	}
}

// Con strip_accents en false el pliegue de diacríticos no se aplica y el texto
// llega al codificador con sus acentos: es lo que se quiere en una ticketera que
// sí respeta el "ESC t n". La "ñ" y el "°" —el caso que salía como varios
// símbolos basura— viajan como un solo byte de CP1252.
func TestBuildPayloadWithoutStrippingAccents(t *testing.T) {
	opts := DefaultEncodingOptions()
	opts.StripAccents = false

	got, err := BuildESCPOSPayload([]byte("Ñoño 20° €"), opts)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	want := []byte{
		0x1B, 0x40, 0x1B, 0x74, 0x10,
		0xD1, 'o', 0xF1, 'o', ' ', '2', '0', 0xB0, ' ', 0x80,
	}
	if !bytes.Equal(got, want) {
		t.Errorf("payload = % X, se esperaba % X", got, want)
	}
}

// El mismo texto con el pliegue activo (el valor por defecto): las eñes pierden
// la virgulilla, y lo que no es una marca combinante —el grado y el euro— sigue
// pasando por el codificador de la página de códigos.
func TestBuildPayloadStripsAccentsButStillTranscodes(t *testing.T) {
	got, err := BuildESCPOSPayload([]byte("Ñoño 20° €"), DefaultEncodingOptions())
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	want := []byte{
		0x1B, 0x40, 0x1B, 0x74, 0x10,
		'N', 'o', 'n', 'o', ' ', '2', '0', 0xB0, ' ', 0x80,
	}
	if !bytes.Equal(got, want) {
		t.Errorf("payload = % X, se esperaba % X", got, want)
	}
}

// escpos_initialize=false deja fuera el "ESC @" sin tocar la selección de
// página: es la válvula de escape para el frontend que gestiona el reinicio por
// su cuenta a mitad de un flujo de varios tickets.
func TestBuildPayloadWithoutInitialize(t *testing.T) {
	opts := DefaultEncodingOptions()
	opts.Initialize = false

	got, err := BuildESCPOSPayload([]byte("TOTAL"), opts)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	want := append([]byte{0x1B, 0x74, 0x10}, []byte("TOTAL")...)
	if !bytes.Equal(got, want) {
		t.Errorf("payload = % X, se esperaba % X", got, want)
	}
}

// El agente no duplica el reinicio: un payload que ya abre con varios "ESC @"
// conserva los suyos y la selección de página entra detrás del último.
func TestBuildPayloadDoesNotDuplicateInitialize(t *testing.T) {
	input := append([]byte{0x1B, 0x40, 0x1B, 0x40}, []byte("OK")...)

	got, err := BuildESCPOSPayload(input, DefaultEncodingOptions())
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	want := []byte{0x1B, 0x40, 0x1B, 0x40, 0x1B, 0x74, 0x10, 'O', 'K'}
	if !bytes.Equal(got, want) {
		t.Errorf("payload = % X, se esperaba % X", got, want)
	}
}

// Un carácter sin representación en la página activa no puede tumbar el ticket
// ni imprimirse como basura: baja a su equivalente ASCII y, si no lo tiene, a
// '?'. Sin pliegue de acentos previo, para que el que llegue al codificador sea
// el texto original.
func TestBuildPayloadReplacesUnmappableRunes(t *testing.T) {
	opts := DefaultEncodingOptions()
	opts.CodePage = "cp437"
	opts.StripAccents = false

	got, err := BuildESCPOSPayload([]byte("漢 ñ"), opts)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	// CP437 no tiene el ideograma, que además no tiene equivalente ASCII en
	// asciiFallback (-> '?'), pero sí la eñe minúscula (0xA4).
	want := []byte{0x1B, 0x40, 0x1B, 0x74, 0x00, '?', ' ', 0xA4}
	if !bytes.Equal(got, want) {
		t.Errorf("payload = % X, se esperaba % X", got, want)
	}
}

// DefaultEncodingOptions es el punto de partida de todo constructor de opciones:
// si alguno de estos valores dejara de ser el esperado, cambiaría en silencio lo
// que se imprime en todas las cajas.
func TestDefaultEncodingOptions(t *testing.T) {
	opts := DefaultEncodingOptions()

	if opts.CodePage != defaultCodePage {
		t.Errorf("CodePage = %q, se esperaba %q", opts.CodePage, defaultCodePage)
	}
	if !opts.Transcode || !opts.StripAccents || !opts.Initialize {
		t.Errorf("los tres tratamientos deben venir activos: %+v", opts)
	}
	if opts.SelectorOverride != nil {
		t.Errorf("SelectorOverride = %v, se esperaba nil", *opts.SelectorOverride)
	}
}
