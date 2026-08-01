package main

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
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
	// Table mapea cada runa Unicode al byte que la representa en la página.
	// Sólo contiene el rango 0x80–0xFF; el ASCII se copia sin traducir.
	Table map[rune]byte
	// Description es la etiqueta legible que se escribe en el log.
	Description string
}

// codePageNone desactiva por completo el tratamiento de codificación: el
// payload viaja al spooler byte por byte, tal cual lo envió el frontend.
const codePageNone = "none"

// defaultCodePage es la página por defecto para tickets en español. CP850
// (Multilingual Latin-1) contiene las cinco vocales acentuadas mayúsculas
// (Á É Í Ó Ú), la Ñ/ñ, la Ü/ü y los signos ¿ ¡ — que CP437, la página por
// defecto de fábrica de la mayoría de ticketeras, NO incluye en mayúsculas.
const defaultCodePage = "cp850"

// supportedCodePages indexa las páginas soportadas por su nombre canónico.
// Los valores de "n" siguen la tabla estándar de Epson ESC/POS, respetada por
// la práctica totalidad de ticketeras compatibles del mercado.
var supportedCodePages = map[string]CodePage{
	"cp850":  {Name: "cp850", Selector: 0x02, Table: codePageCP850, Description: "PC850 Multilingual (Latin-1)"},
	"cp858":  {Name: "cp858", Selector: 0x13, Table: codePageCP858, Description: "PC858 Euro (Latin-1 + €)"},
	"cp1252": {Name: "cp1252", Selector: 0x10, Table: codePageCP1252, Description: "WPC1252 (Windows Latin-1)"},
	"cp437":  {Name: "cp437", Selector: 0x00, Table: codePageCP437, Description: "PC437 USA/Standard Europe"},
}

// codePageAliases acepta las formas en que un frontend puede nombrar una
// página de códigos, para no obligarlo a conocer el identificador exacto.
var codePageAliases = map[string]string{
	"850": "cp850", "pc850": "cp850", "ibm850": "cp850", "latin1": "cp850", "multilingual": "cp850",
	"858": "cp858", "pc858": "cp858", "ibm858": "cp858", "euro": "cp858",
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

// BuildESCPOSPayload prepara los bytes definitivos que se escriben en la
// impresora térmica:
//
//  1. Transcodifica el texto UTF-8 a los bytes de la página de códigos del
//     hardware (Á É Í Ó Ú Ñ Ü ¿ ¡ …), que de otro modo se imprimirían como
//     pares de caracteres basura porque la ticketera interpreta cada byte
//     UTF-8 por separado.
//  2. Antepone el comando "ESC t n" para activar esa misma página de códigos
//     en la impresora, respetando un "ESC @" inicial si el payload lo trae.
//
// Si el payload ya contiene su propio "ESC t", se asume que el emisor gestiona
// la codificación y los bytes se devuelven intactos.
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

	payload := data
	if opts.Transcode {
		payload = transcodeToCodePage(payload, cp.Table)
	}

	return insertCodePageCommand(payload, cp.Selector), nil
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

// insertCodePageCommand inyecta "ESC t n" al principio del payload, pero por
// detrás de los "ESC @" iniciales: ese comando reinicia la impresora y
// restauraría la página de códigos de fábrica, anulando la selección.
func insertCodePageCommand(data []byte, selector byte) []byte {
	offset := 0
	for offset+1 < len(data) && data[offset] == escInitialize[0] && data[offset+1] == escInitialize[1] {
		offset += 2
	}

	command := []byte{escSelectCodeTable[0], escSelectCodeTable[1], selector}

	out := make([]byte, 0, len(data)+len(command))
	out = append(out, data[:offset]...)
	out = append(out, command...)
	out = append(out, data[offset:]...)
	return out
}

// transcodeToCodePage convierte las secuencias UTF-8 del payload a los bytes de
// la página de códigos indicada.
//
// El recorrido es conservador a propósito, porque un ticket RAW mezcla texto con
// comandos y con datos binarios (logos, imágenes raster):
//
//   - Los comandos gráficos se detectan por su cabecera y sus datos binarios se
//     copian en bloque sin interpretarlos (ver graphicsCommandLength).
//   - Los bytes ASCII (< 0x80) se copian sin tocar: son idénticos en las cuatro
//     páginas y cubren todos los comandos ESC/POS.
//   - Las secuencias UTF-8 válidas se traducen por su byte equivalente; si la
//     runa no existe en la página de códigos se usa su aproximación ASCII
//     (asciiFallback) y, en último caso, '?'.
//   - Los bytes sueltos que no forman UTF-8 válido se copian tal cual.
func transcodeToCodePage(data []byte, table map[rune]byte) []byte {
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

		if mapped, ok := table[r]; ok {
			out = append(out, mapped)
		} else if fallback, ok := asciiFallback[r]; ok {
			out = append(out, fallback...)
		} else {
			out = append(out, '?')
		}
		i += size
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
