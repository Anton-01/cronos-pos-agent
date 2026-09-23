package main

import (
	"bytes"
	"fmt"
	"testing"
)

// El emparejamiento por modelo es lo que ahorra el ticket de calibración en el
// hardware cuyo comportamiento no está en duda. Tiene que ser insensible a
// mayúsculas y al ruido que Windows añade al nombre de la cola.
func TestProfileFromModel(t *testing.T) {
	cases := []struct {
		printer      string
		wantCodePage string
		wantVerified bool
	}{
		{"EPSON TM-T20III Receipt", "cp858", true},
		{"epson tm-t88vi", "cp858", true},
		{"BIXOLON SRP-350III", "cp858", true},
		{"Star TSP143III", "cp858", true},
		// Un nombre que no identifica ningún firmware conocido se queda sin
		// perfil: mejor cuatro caracteres plegados que un ticket ilegible.
		{"POS-58", "", false},
		{"Impresora de tickets", "", false},
		{"XP-80C", "", false},
		{"", "", false},
	}

	for _, tc := range cases {
		got := profileFromModel(tc.printer)
		if got.CodePage != tc.wantCodePage {
			t.Errorf("profileFromModel(%q) página = %q, se esperaba %q", tc.printer, got.CodePage, tc.wantCodePage)
		}
		if got.Verified != tc.wantVerified {
			t.Errorf("profileFromModel(%q) verificada = %v, se esperaba %v", tc.printer, got.Verified, tc.wantVerified)
		}
		if tc.wantVerified && got.Source != profileSourceModel {
			t.Errorf("profileFromModel(%q) origen = %q, se esperaba %q", tc.printer, got.Source, profileSourceModel)
		}
	}
}

// Dos perfiles para la misma impresora harían que el ganador dependiera del
// orden de iteración de un mapa, así que la clave se normaliza.
func TestNormalizePrinterKey(t *testing.T) {
	same := []string{"EPSON TM-T20", "epson tm-t20", "  Epson TM-T20  "}
	first := normalizePrinterKey(same[0])
	for _, name := range same[1:] {
		if normalizePrinterKey(name) != first {
			t.Errorf("normalizePrinterKey(%q) = %q, se esperaba %q", name, normalizePrinterKey(name), first)
		}
	}
	if normalizePrinterKey("EPSON TM-T88") == first {
		t.Error("dos impresoras distintas no pueden compartir clave")
	}
}

// Las opciones se numeran de 1 en adelante y sin huecos: ese número es lo único
// que el operador lee del papel y lo único que devuelve al agente, así que una
// numeración con saltos guardaría el perfil equivocado.
func TestCalibrationOptionsAreNumberedInOrder(t *testing.T) {
	options := CalibrationOptions()
	if len(options) != len(supportedCodePages)+1 {
		t.Fatalf("hay %d opciones, se esperaba una por página más la de fábrica", len(options))
	}

	for i, option := range options {
		if option.Option != i+1 {
			t.Errorf("la opción en la posición %d está numerada como %d", i, option.Option)
		}
		if _, _, err := ResolveCodePage(option.CodePage); err != nil {
			t.Errorf("la opción %d nombra una página no soportada: %v", option.Option, err)
		}
	}

	// La primera es la que se imprime sin seleccionar página: es la que revela
	// qué hace la impresora con la tabla que arrancó, que es justo el dato que
	// falta cuando el hardware ignora el "ESC t n".
	if options[0].Selector != nil {
		t.Error("la primera opción debe imprimirse sin ESC t")
	}

	for _, option := range options[1:] {
		if option.Selector == nil {
			t.Errorf("la opción %d debería llevar su ESC t", option.Option)
		}
	}
}

// El ticket de prueba es la única forma de saber qué decodifica una impresora,
// así que tiene que llevar de verdad cada candidato con su selección delante.
func TestBuildCalibrationTicket(t *testing.T) {
	ticket := BuildCalibrationTicket()

	if !bytes.HasPrefix(ticket, escInitialize) {
		t.Error("el ticket debe abrir con ESC @: sin reinicio se estaría midiendo el estado anterior")
	}
	if !bytes.HasSuffix(ticket, []byte{0x1D, 0x56, 0x00}) {
		t.Error("el ticket debe cerrar con el corte de papel")
	}

	for _, option := range CalibrationOptions() {
		marker := []byte(fmt.Sprintf("[%d] ", option.Option))
		index := bytes.Index(ticket, marker)
		if index < 0 {
			t.Errorf("falta la línea de la opción %d", option.Option)
			continue
		}
		if option.Selector == nil {
			continue
		}
		selection := []byte{escSelectCodeTable[0], escSelectCodeTable[1], byte(*option.Selector)}
		if !bytes.Contains(ticket[:index], selection) {
			t.Errorf("la opción %d no lleva su ESC t %d delante", option.Option, *option.Selector)
		}
	}

	// La línea de CP850 tiene que llevar el 0xB5 de la "Á": es precisamente el
	// byte que en PC437 se imprime como "╡", de modo que el operador ve el
	// defecto y la alternativa correcta en el mismo papel.
	if !bytes.Contains(ticket, []byte{0xB5}) {
		t.Error("el ticket no lleva la 'Á' de CP850 (0xB5), que es el caso que hay que distinguir")
	}

	// Y tiene que volver a dejar la impresora en la página por defecto del
	// agente, no en la del último candidato impreso.
	cp, _, err := ResolveCodePage(defaultCodePage)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	restore := []byte{escSelectCodeTable[0], escSelectCodeTable[1], cp.Selector}
	if !bytes.Contains(ticket[bytes.LastIndex(ticket, []byte("[")):], restore) {
		t.Error("el ticket debe restaurar la página por defecto al final")
	}
}

// El texto de instrucciones tiene que leerse incluso en la impresora que falla
// todos los candidatos, que es justo la que más lo necesita.
func TestCalibrationTicketInstructionsAreASCII(t *testing.T) {
	ticket := BuildCalibrationTicket()
	sample := []byte(fmt.Sprintf("[%d] ", 1))

	header := ticket[:bytes.Index(ticket, sample)]
	for i, b := range header {
		if b >= 0x80 {
			t.Fatalf("la cabecera lleva un byte no ASCII (0x%02X) en la posición %d", b, i)
		}
	}
}
