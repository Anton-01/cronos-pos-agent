package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Per-printer encoding profiles.
//
// A code page is a property of the printer, not of the installation: a shop can
// have a genuine Epson at the counter and a clone at the kitchen, and the single
// global "escpos_code_page" of config.json is necessarily wrong for one of them.
// A profile records what the agent knows about one printer, and that knowledge
// comes from one of three places, in descending order of authority:
//
//   - calibration: the operator printed the test ticket (see
//     BuildCalibrationTicket) and told the agent which of the numbered lines
//     read correctly. That is the only way to *know* what a printer does, since
//     ESC/POS has no command to ask a printer which table it has active.
//   - model: the printer's name matched one of the models known to honour
//     "ESC t n" (see knownPrinterModels). It saves the operator the calibration
//     ticket on the hardware where the answer is not in doubt.
//   - manual: somebody wrote the profile into config.json by hand.
//
// A printer with no profile is not a failure: it prints in compatibility mode,
// which is correct on every printer and only loses the accent on Á Í Ó Ú. The
// profile is what buys those four characters back.

// PrinterProfile is what the agent has established about one printer's encoding.
type PrinterProfile struct {
	// CodePage is the canonical name of the table this printer decodes with.
	CodePage string `json:"code_page"`
	// CodePageID overrides the "n" of "ESC t n" for this printer alone, for the
	// clones that number their pages differently from the Epson standard. Nil
	// uses the standard numbering.
	CodePageID *int `json:"code_page_id,omitempty"`
	// Verified marks a profile whose code page was actually confirmed on paper
	// (or is beyond doubt from the model). Only a verified profile turns
	// compatibility mode off, which is what allows Á Í Ó Ú to be printed for
	// real instead of folded to ASCII.
	Verified bool `json:"verified"`
	// Source records where the profile came from: "calibration", "model" or
	// "manual". It is written for the operator reading config.json, and it is
	// also what tells a profile the agent inferred from one a human confirmed.
	Source string `json:"source,omitempty"`
	// UpdatedAt is the RFC3339 timestamp of the last change.
	UpdatedAt string `json:"updated_at,omitempty"`
}

// profileSourceCalibration and friends are the accepted values of
// PrinterProfile.Source.
const (
	profileSourceCalibration = "calibration"
	profileSourceModel       = "model"
	profileSourceManual      = "manual"
)

// knownPrinterModels maps a substring of the printer's name —the one Windows
// shows in the spooler, which carries the model— to the code page that model is
// documented to support. The match saves the calibration ticket on the hardware
// whose behaviour is not in question.
//
// The list is deliberately short and conservative. A name is weak evidence: a
// queue called "POS-58" says nothing about the firmware behind it, and only a
// manufacturer that publishes its ESC/POS command set belongs here. Everything
// else stays unverified and prints in compatibility mode, which is right on any
// printer — being cautious costs four folded characters, being wrong costs a
// ticket the customer cannot read.
var knownPrinterModels = []struct {
	Pattern     string
	CodePage    string
	Description string
}{
	{"epson tm-", "cp858", "Epson TM series"},
	{"epson tm_", "cp858", "Epson TM series"},
	{"bixolon srp", "cp858", "Bixolon SRP series"},
	{"star tsp", "cp858", "Star TSP series"},
	{"star mc-", "cp858", "Star mC series"},
}

// PrinterProfileFor returns what the agent knows about a printer: the profile
// saved in config.json if there is one, otherwise the one its model implies,
// otherwise an empty profile (unverified, no code page) meaning "nothing known,
// print in compatibility mode".
//
// A saved profile always wins over the model match, because the operator who
// ran the calibration ticket was looking at the paper and the model table is
// only reading a name.
func PrinterProfileFor(printerName string) PrinterProfile {
	key := normalizePrinterKey(printerName)
	if key == "" {
		return PrinterProfile{}
	}

	if cfg, err := LoadConfig(); err == nil {
		for saved, profile := range cfg.Printers {
			if normalizePrinterKey(saved) == key {
				return profile
			}
		}
	}

	return profileFromModel(printerName)
}

// profileFromModel matches a printer's name against knownPrinterModels. An
// unknown name returns the empty profile, which is the safe answer.
func profileFromModel(printerName string) PrinterProfile {
	name := strings.ToLower(strings.TrimSpace(printerName))
	if name == "" {
		return PrinterProfile{}
	}

	for _, model := range knownPrinterModels {
		if strings.Contains(name, model.Pattern) {
			return PrinterProfile{
				CodePage: model.CodePage,
				Verified: true,
				Source:   profileSourceModel,
			}
		}
	}

	return PrinterProfile{}
}

// SavePrinterProfile writes a printer's profile to config.json, replacing any
// previous one for the same printer. It is what the calibration confirmation
// endpoint calls once the operator has told the agent which line of the test
// ticket printed correctly.
func SavePrinterProfile(printerName string, profile PrinterProfile) error {
	key := strings.TrimSpace(printerName)
	if key == "" {
		return fmt.Errorf("el nombre de la impresora es obligatorio")
	}
	if _, _, err := ResolveCodePage(profile.CodePage); err != nil {
		return err
	}
	if profile.CodePageID != nil && (*profile.CodePageID < 0 || *profile.CodePageID > 255) {
		return fmt.Errorf("code_page_id=%d fuera de rango (0–255)", *profile.CodePageID)
	}

	if _, err := LoadConfig(); err != nil {
		return err
	}

	configMu.Lock()
	defer configMu.Unlock()

	if appConfig.Printers == nil {
		appConfig.Printers = make(map[string]PrinterProfile, 1)
	}
	// A printer already stored under a differently-cased or padded name is
	// replaced rather than duplicated: two profiles for one printer would make
	// the winner depend on map iteration order.
	normalized := normalizePrinterKey(key)
	for saved := range appConfig.Printers {
		if normalizePrinterKey(saved) == normalized {
			delete(appConfig.Printers, saved)
		}
	}

	profile.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	appConfig.Printers[key] = profile

	return saveConfig(configFilePath())
}

// normalizePrinterKey folds the differences that do not identify a printer —
// case and surrounding blanks — so that "EPSON TM-T20" and "epson tm-t20  " are
// recognised as the same queue.
func normalizePrinterKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// CalibrationOption is one numbered line of the calibration ticket: a code page,
// the "ESC t n" that selects it, and the label the operator reads off the paper.
type CalibrationOption struct {
	// Option is the number printed at the start of the line, which is what the
	// operator sends back to /api/print/calibrate/confirm.
	Option int `json:"option"`
	// CodePage is the canonical page name this line was encoded with.
	CodePage string `json:"code_page"`
	// Selector is the "n" of the "ESC t n" that precedes the line, or nil for
	// the line printed without any selection at all.
	Selector *int `json:"selector,omitempty"`
	// Description is the human label, in Spanish, shown by the frontend.
	Description string `json:"description"`
}

// calibrationSample is the string every line of the test ticket prints. It
// carries the four characters the code pages disagree on (Á Í Ó Ú) plus the ones
// they agree on (É Ü Ñ ¿ ¡ á é í ó ú ñ), so that a wrong line is obvious at a
// glance: on the wrong page the first group comes out as box-drawing symbols
// while the second still reads correctly, which is exactly the "╡nimo" defect
// this whole mechanism exists to remove.
const calibrationSample = "ÁÉÍÓÚ ÜÑ ¿¡ áéíóú üñ"

// CalibrationOptions lists the candidates printed by the test ticket, in the
// order they appear on paper.
//
// Option 1 is deliberately the one with no "ESC t n" at all: it shows what the
// printer does with the page it booted with, which is the single most useful
// piece of information about hardware that ignores the selection command. The
// rest are the four supported pages with their standard Epson numbering.
func CalibrationOptions() []CalibrationOption {
	selector := func(v byte) *int { n := int(v); return &n }

	options := []CalibrationOption{
		{Option: 1, CodePage: "cp858", Selector: nil, Description: "Sin seleccionar página (la de fábrica de la impresora)"},
	}

	names := make([]string, 0, len(supportedCodePages))
	for name := range supportedCodePages {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		cp := supportedCodePages[name]
		options = append(options, CalibrationOption{
			Option:      len(options) + 1,
			CodePage:    cp.Name,
			Selector:    selector(cp.Selector),
			Description: fmt.Sprintf("%s — ESC t %d", cp.Description, cp.Selector),
		})
	}

	return options
}

// BuildCalibrationTicket assembles the RAW ESC/POS bytes of the test ticket: the
// same reference string printed once per candidate, each line numbered and each
// preceded by the "ESC t n" of its page.
//
// It is the answer to a limitation of the protocol rather than a convenience.
// ESC/POS has no command that asks a printer which character table it has
// active, so the agent cannot find out by itself what the hardware in front of
// it does with "ESC t n" — it can only print every possibility and let the
// person holding the paper read the answer. One look at the ticket tells the
// operator which line shows "ÁÉÍÓÚ" instead of box-drawing symbols, and that
// number, sent back to /api/print/calibrate/confirm, is what verifies the
// printer for good.
//
// Everything around the samples is written in plain ASCII on purpose: the
// instructions have to be readable even on the printer that gets every single
// candidate wrong.
func BuildCalibrationTicket() []byte {
	var out []byte

	// ESC @ — start from a known state, so that nothing left over from the
	// previous ticket is mistaken for the behaviour being measured.
	out = append(out, escInitialize...)

	// ESC a 1 / ESC a 0 — centre the header, then back to the left for the body.
	out = append(out, 0x1B, 0x61, 0x01)
	out = append(out, []byte("CALIBRACION DE ACENTOS\n")...)
	out = append(out, []byte("Cronos POS Agent "+AgentVersion+"\n")...)
	out = append(out, 0x1B, 0x61, 0x00)
	out = append(out, []byte("\n")...)
	out = append(out, []byte("Busque la linea que imprime\n")...)
	out = append(out, []byte("correctamente: A E I O U con\n")...)
	out = append(out, []byte("acento, y la enye mayuscula.\n")...)
	out = append(out, []byte("Anote su numero.\n")...)
	out = append(out, []byte(strings.Repeat("-", 32)+"\n")...)

	for _, option := range CalibrationOptions() {
		cp, _, err := ResolveCodePage(option.CodePage)
		if err != nil {
			continue
		}

		if option.Selector != nil {
			out = append(out, escSelectCodeTable[0], escSelectCodeTable[1], byte(*option.Selector))
		}
		out = append(out, []byte(fmt.Sprintf("[%d] ", option.Option))...)
		out = append(out, transcodeToCodePage([]byte(calibrationSample), cp.Charmap)...)
		out = append(out, '\n')
	}

	// Back to the page the agent uses by default, so the ticket does not leave
	// the printer sitting on whichever candidate happened to be printed last.
	if cp, active, err := ResolveCodePage(defaultCodePage); err == nil && active {
		out = append(out, escSelectCodeTable[0], escSelectCodeTable[1], cp.Selector)
	}

	out = append(out, []byte(strings.Repeat("-", 32)+"\n")...)
	out = append(out, []byte("Si ninguna linea es correcta,\n")...)
	out = append(out, []byte("esta impresora no puede imprimir\n")...)
	out = append(out, []byte("A E I O U con acento y el agente\n")...)
	out = append(out, []byte("las imprimira sin el.\n")...)
	out = append(out, []byte("\n\n\n")...)

	// GS V 0 — full cut. A printer without a cutter ignores it.
	out = append(out, 0x1D, 0x56, 0x00)

	return out
}
