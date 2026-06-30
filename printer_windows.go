//go:build windows

package main

import (
	"fmt"

	"github.com/alexbrainman/printer"
)

func discoverPrinters() ([]PrinterInfo, error) {
	names, err := printer.ReadNames()
	if err != nil {
		return nil, fmt.Errorf("error leyendo impresoras del spooler: %w", err)
	}

	printers := make([]PrinterInfo, 0, len(names))
	for _, name := range names {
		printers = append(printers, PrinterInfo{Name: name})
	}
	return printers, nil
}

func rawPrint(printerName string, data []byte) error {
	p, err := printer.Open(printerName)
	if err != nil {
		return fmt.Errorf("no se pudo abrir la impresora '%s': %w", printerName, err)
	}
	defer p.Close()

	if err := p.StartRawDocument("CronosTicket"); err != nil {
		return fmt.Errorf("error al iniciar documento RAW: %w", err)
	}

	if err := p.StartPage(); err != nil {
		return fmt.Errorf("error al iniciar página: %w", err)
	}

	if _, err := p.Write(data); err != nil {
		return fmt.Errorf("error al escribir datos ESC/POS: %w", err)
	}

	if err := p.EndPage(); err != nil {
		return fmt.Errorf("error al finalizar página: %w", err)
	}

	if err := p.EndDocument(); err != nil {
		return fmt.Errorf("error al finalizar documento: %w", err)
	}

	return nil
}
