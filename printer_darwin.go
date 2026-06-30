//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func discoverPrinters() ([]PrinterInfo, error) {
	out, err := exec.Command("lpstat", "-a").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("error ejecutando lpstat: %w (salida: %s)", err, string(out))
	}

	var printers []PrinterInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		name := strings.Fields(line)[0]
		printers = append(printers, PrinterInfo{Name: name})
	}
	return printers, nil
}

func rawPrint(printerName string, data []byte) error {
	tmpFile, err := os.CreateTemp("", "cronos-ticket-*.bin")
	if err != nil {
		return fmt.Errorf("error creando archivo temporal: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("error escribiendo datos al archivo temporal: %w", err)
	}
	tmpFile.Close()

	out, err := exec.Command("lp", "-d", printerName, "-o", "raw", tmpFile.Name()).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error enviando a impresora '%s': %w (salida: %s)", printerName, err, string(out))
	}

	return nil
}
