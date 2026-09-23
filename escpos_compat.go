package main

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// Compatibility mode: printing text that is right whatever code page the
// printer ended up with.
//
// The problem it solves. Selecting a code page with "ESC t n" is a request, not
// a contract. Three things have to agree for an accented character to reach the
// paper intact, and the agent only controls the first:
//
//  1. the bytes the agent writes to the spooler,
//  2. the table the printer's firmware has active when it decodes them,
//  3. the glyphs the printer's resident font ROM actually contains.
//
// A large part of the hardware in the field ignores "ESC t n" outright, numbers
// its pages differently from the Epson standard, or simply has no table beyond
// the PC437 it booted with. On those printers there is no byte value that
// prints "Á", so no amount of code page cleverness fixes the ticket.
//
// The observation this mode is built on. PC437, PC850 and PC858 —the three
// tables that cover the entire installed base of ESC/POS hardware— do not
// disagree everywhere. They assign the *same byte* to 81 of the characters
// above ASCII, and among them are almost all the ones Spanish needs:
//
//	á A0   é 82   í A1   ó A2   ú A3   ü 81   ñ A4
//	Ñ A5   É 90   Ü 9A   ¿ A8   ¡ AD   º A7   ª A6   ° F8
//
// For that subset the selection of the code page is irrelevant: whichever of
// the three tables the printer has active, the byte decodes to the same glyph.
// What the three tables disagree on, in Spanish, is exactly four characters —
// Á, Í, Ó, Ú — which PC437 does not contain at all, so PC850's bytes for them
// (B5, D6, E0, E9) land on the box-drawing glyphs ╡, ╓, α, ┘ of PC437. That is
// the "╡nimo" printed instead of "Ánimo".
//
// What this mode does. It encodes the ticket against that universally safe
// subset: the characters in it travel as their real byte and print correctly on
// any printer, and the handful outside it are folded to ASCII (Á→A) instead of
// being emitted as a byte whose meaning depends on hardware nobody surveyed.
// The ticket then cannot print garbage on any printer, ever — the worst case it
// can produce is "Animo" with "Michoacán" still perfectly accented, instead of
// today's "╡nimo".
//
// It is the safe default for a printer the agent has never seen. Once a printer
// is verified —by the calibration ticket or by its model— its profile turns the
// mode off and the four remaining characters are printed for real.

// compatibilityPages are the tables whose agreement defines the safe subset.
// They are the three the ESC/POS market actually ships: PC437 is the factory
// page of nearly every printer, PC850 the multilingual one every clone adds and
// PC858 is PC850 plus the euro sign.
//
// Windows-1252 is deliberately NOT here. It is a Latin-1 layout and shares no
// byte with the DOS pages above ASCII (its "á" is E1, not A0), so a printer that
// ignores "ESC t 16" and keeps decoding PC437 turns *every* accent into garbage
// —"Michoacßn"— instead of only the four uppercase vowels. That is why
// compatibility mode also pins the encoding to PC858 (see compatibilityCodePage)
// rather than honouring a CP1252 configuration it cannot make safe.
var compatibilityPages = []*charmap.Charmap{codePageCP437, codePageCP850, codePageCP858}

// compatibilityCodePage is the table the payload is encoded against while the
// mode is active. PC858 is a superset of the safe subset and the widest of the
// three, so anything that survives the fold is encoded at its real byte and the
// selection ("ESC t 19") is still worth sending for the printers that honour it.
const compatibilityCodePage = "cp858"

// universalSafeRunes maps every rune that the tables in compatibilityPages
// encode to one and the same byte. It is computed at start-up from the charmaps
// themselves rather than written out by hand, so adding a page to
// compatibilityPages re-derives the subset instead of inviting a transcription
// error in an 81-entry table.
var universalSafeRunes = buildUniversalSafeRunes(compatibilityPages...)

// buildUniversalSafeRunes walks the high half of the byte range (0x80–0xFF; the
// low half is ASCII and identical in every page by definition) and keeps the
// bytes that decode to the very same rune in all the given pages. A byte the
// pages disagree on is dropped, and with it both of its meanings: the ambiguity
// is the defect, so neither "Á" (B5 in PC850) nor "╡" (B5 in PC437) is safe.
func buildUniversalSafeRunes(pages ...*charmap.Charmap) map[rune]byte {
	safe := make(map[rune]byte, 96)
	if len(pages) == 0 {
		return safe
	}

	for value := 0x80; value <= 0xFF; value++ {
		b := byte(value)

		r := pages[0].DecodeByte(b)
		if r == utf8.RuneError {
			continue // the first page does not map this byte
		}

		agreed := true
		for _, page := range pages[1:] {
			if page.DecodeByte(b) != r {
				agreed = false
				break
			}
		}
		if agreed {
			safe[r] = b
		}
	}

	return safe
}

// isUniversallySafe reports whether a rune prints the same on every printer
// regardless of which of the three code pages is active. ASCII always is: the
// 0x00–0x7F range is shared by every ESC/POS table, and it is also where all the
// commands live.
func isUniversallySafe(r rune) bool {
	if r < utf8.RuneSelf {
		return true
	}
	_, ok := universalSafeRunes[r]
	return ok
}

// foldToUniversalSafe rewrites a string so that every rune in it is one the
// printer cannot get wrong. Safe runes —which is to say every accented
// character Spanish uses except Á, Í, Ó and Ú— are kept exactly as they are; the
// rest are degraded, in order of preference, to their ASCII fallback
// (`Á`→`A`, `€`→`EUR`, `…`→`...`), to their base letter with the diacritic
// dropped, and failing both to '?'.
//
// The difference with sanitizeTextForPrinter is the whole point of this mode:
// that one folds *every* diacritic, so a ticket comes out as "Animo" and
// "Michoacan"; this one folds only what is genuinely ambiguous, so the same
// ticket keeps "Michoacán" and loses the accent on four characters only.
func foldToUniversalSafe(input string) string {
	var out strings.Builder
	out.Grow(len(input))

	for _, r := range input {
		if isUniversallySafe(r) {
			out.WriteRune(r)
			continue
		}

		if fallback, ok := asciiFallback[r]; ok {
			out.WriteString(fallback)
			continue
		}

		// No curated fallback: strip the diacritics and keep whatever base
		// letter is left, which is ASCII and therefore safe by construction.
		if folded := sanitizeTextForPrinter(string(r)); folded != "" && isASCIIOnly(folded) {
			out.WriteString(folded)
			continue
		}

		out.WriteByte('?')
	}

	return out.String()
}

// isASCIIOnly reports whether every byte of s is below 0x80. It guards the
// diacritic fold in foldToUniversalSafe: a rune whose decomposition is still
// non-ASCII (a Cyrillic or CJK character, say) has not been made safe by the
// fold and has to fall through to '?'.
func isASCIIOnly(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// foldPayloadToUniversalSafe applies foldToUniversalSafe to every stretch of
// real text in a RAW ESC/POS payload, leaving commands and binary data alone. It
// walks the buffer with mapTextRuns, the same reader the transcoder and the
// diacritic fold use, so a logo's raster bytes are never mistaken for text.
func foldPayloadToUniversalSafe(data []byte) []byte {
	return mapTextRuns(data, func(run []byte) []byte {
		return []byte(foldToUniversalSafe(string(run)))
	})
}
