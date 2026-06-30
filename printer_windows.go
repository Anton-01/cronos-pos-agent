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
