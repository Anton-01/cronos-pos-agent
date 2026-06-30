//go:build darwin

package main

import (
	"fmt"
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
		// lpstat -a format: "PrinterName accepting requests since ..."
		name := strings.Fields(line)[0]
		printers = append(printers, PrinterInfo{Name: name})
	}
	return printers, nil
}
