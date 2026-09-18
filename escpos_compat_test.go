package main

import (
	"bytes"
	"testing"

	"golang.org/x/text/encoding/charmap"
)

// El subconjunto seguro es la base de todo el modo compatible: si dejara de
// contener alguno de estos caracteres, un ticket perfectamente imprimible
// empezaría a salir sin acentos; si contuviera alguno de los cuatro
// conflictivos, volvería el "╡nimo" que este mecanismo existe para eliminar.
func TestUniversalSafeSubset(t *testing.T) {
	for _, r := range "áéíóúüñÑÉÜ¿¡ºª°" {
		if !isUniversallySafe(r) {
			t.Errorf("%q debería ser universalmente segura: PC437, PC850 y PC858 la codifican igual", r)
		}
	}

	// PC437 no contiene estas cuatro, así que el byte que PC850 y PC858 les dan
	// cae sobre un carácter de dibujo suyo. Son exactamente las que fallaban.
	for _, r := range "ÁÍÓÚ€" {
		if isUniversallySafe(r) {
			t.Errorf("%q no puede considerarse segura: las tres páginas no coinciden en su byte", r)
		}
	}

	// El ASCII es la única parte del rango que todas las tablas comparten por
	// definición, y es donde viven además todos los comandos ESC/POS.
	for _, r := range "ABCabc0123 $#*" {
		if !isUniversallySafe(r) {
			t.Errorf("%q es ASCII y debe ser segura", r)
		}
	}
}

// Propiedad que define el conjunto: cada runa que declara segura tiene que
// codificarse al mismo byte en las tres páginas. Se comprueba contra los
// charmaps en vez de contra una tabla escrita a mano, que es lo que permite
// añadir una página a compatibilityPages sin revisar 81 entradas.
func TestUniversalSafeRunesAgreeAcrossPages(t *testing.T) {
	if len(universalSafeRunes) == 0 {
		t.Fatal("el subconjunto seguro está vacío")
	}

	for r, want := range universalSafeRunes {
		for _, page := range compatibilityPages {
			got, ok := page.EncodeRune(r)
			if !ok {
				t.Errorf("%q se declara segura pero una de las páginas no la codifica", r)
				continue
			}
			if got != want {
				t.Errorf("%q = 0x%02X en una página y 0x%02X en otra: no es segura", r, got, want)
			}
		}
	}
}

// El pliegue selectivo es lo que distingue este modo del plegado total: conserva
// todo lo que la impresora no puede equivocar y solo degrada lo que sí.
func TestFoldToUniversalSafe(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Ánimo, ya falta menos para navidad", "Animo, ya falta menos para navidad"},
		{"Chapultepec Sur, Morelia, Michoacán", "Chapultepec Sur, Morelia, Michoacán"},
		{"ÁÉÍÓÚ", "AÉIOU"},
		{"ÑOÑO niño", "ÑOÑO niño"},
		{"¿Cuánto? ¡Órale!", "¿Cuánto? ¡Orale!"},
		{"20° 1ª 2º", "20° 1ª 2º"},
		{"Total 95.00 €", "Total 95.00 EUR"},
		{"漢字", "??"},
	}

	for _, tc := range cases {
		if got := foldToUniversalSafe(tc.in); got != tc.want {
			t.Errorf("foldToUniversalSafe(%q) = %q, se esperaba %q", tc.in, got, tc.want)
		}
	}
}

// Un ticket no es una cadena: el pliegue tiene que saltarse los datos binarios
// de un logo o descuadraría la longitud declarada del comando y la impresora
// interpretaría el resto del ticket como órdenes.
func TestFoldPayloadSkipsRasterImageData(t *testing.T) {
	// GS v 0 m xL xH yL yH + 2 bytes de datos que forman un UTF-8 válido.
	raster := []byte{0x1D, 0x76, 0x30, 0x00, 0x01, 0x00, 0x02, 0x00, 0xC3, 0x81}
	input := append(append([]byte("Á"), raster...), []byte("Á")...)

	got := foldPayloadToUniversalSafe(input)
	want := append(append([]byte("A"), raster...), []byte("A")...)

	if !bytes.Equal(got, want) {
		t.Errorf("payload = % X, se esperaba % X", got, want)
	}
}

// El modo compatible está definido contra los bytes de la familia DOS, así que
// tiene que imponer PC858 aunque la configuración diga CP1252: codificar en
// Latin-1 dejaría el pliegue pagando por una garantía que ya no da.
func TestCompatibilityPinsCP858(t *testing.T) {
	opts := DefaultEncodingOptions()
	opts.CodePage = "cp1252"

	got, err := BuildESCPOSPayload([]byte("ñ"), opts)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	want := []byte{0x1B, 0x40, 0x1B, 0x74, 0x13, 0xA4}
	if !bytes.Equal(got, want) {
		t.Errorf("payload = % X, se esperaba % X (PC858, no CP1252)", got, want)
	}
}

// La prueba que resume el objetivo del modo: el mismo payload, leído con
// cualquiera de las tres tablas que puede tener activa la impresora, dice
// exactamente lo mismo. Es la definición operativa de "robusto" aquí — el
// agente deja de depender de que el hardware respete el "ESC t n".
func TestTicketReadsTheSameOnEveryPrinterPage(t *testing.T) {
	ticket := "Ánimo, ya falta menos para navidad\n" +
		"Chapultepec Sur, Morelia, Michoacán\n" +
		"¿Efectivo? ¡Buen provecho! 20° ñÑ"

	payload, err := BuildESCPOSPayload([]byte(ticket), DefaultEncodingOptions())
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	// Fuera el preámbulo "ESC @ ESC t n": lo que se comprueba es el texto.
	text := payload[5:]

	want := "Animo, ya falta menos para navidad\n" +
		"Chapultepec Sur, Morelia, Michoacán\n" +
		"¿Efectivo? ¡Buen provecho! 20° ñÑ"

	for _, page := range []*charmap.Charmap{codePageCP437, codePageCP850, codePageCP858} {
		decoded, err := page.NewDecoder().Bytes(text)
		if err != nil {
			t.Fatalf("no se pudo decodificar: %v", err)
		}
		if string(decoded) != want {
			t.Errorf("leído como %s = %q\nse esperaba %q", pageName(page), decoded, want)
		}
	}
}

// pageName da un nombre legible a un charmap para los mensajes de error.
func pageName(page *charmap.Charmap) string {
	for name, cp := range supportedCodePages {
		if cp.Charmap == page {
			return name
		}
	}
	return "desconocida"
}

// Un "ESC @" a mitad del ticket devuelve la impresora a su página de fábrica, y
// con ella se lleva la selección hecha en la cabecera: todo lo impreso después
// saldría con la tabla equivocada. Tres bytes detrás de cada reinicio cierran
// ese agujero.
func TestReassertCodePageAfterMidTicketReset(t *testing.T) {
	input := append([]byte("Café"), append([]byte{0x1B, 0x40}, []byte("Total")...)...)

	opts := DefaultEncodingOptions()
	opts.Compatibility = false

	got, err := BuildESCPOSPayload(input, opts)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	want := []byte{0x1B, 0x40, 0x1B, 0x74, 0x13, 'C', 'a', 'f', 0x82, 0x1B, 0x40, 0x1B, 0x74, 0x13}
	want = append(want, []byte("Total")...)

	if !bytes.Equal(got, want) {
		t.Errorf("payload = % X, se esperaba % X", got, want)
	}
}

// Dos bytes cualesquiera de un logo pueden valer 0x1B 0x40 por casualidad:
// inyectar una selección ahí corrompería la imagen y descuadraría el ticket
// entero.
func TestReassertSkipsResetsInsideRasterData(t *testing.T) {
	raster := []byte{0x1D, 0x76, 0x30, 0x00, 0x02, 0x00, 0x01, 0x00, 0x1B, 0x40}
	got := reassertAfterResets(raster, 0x13)

	if !bytes.Equal(got, raster) {
		t.Errorf("los datos de la imagen fueron modificados: % X", got)
	}
}
