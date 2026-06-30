//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

func queryPrintQueue(printerName string) (QueueInfo, error) {
	out, err := exec.Command("lpstat", "-W", "not-completed", "-o", printerName).CombinedOutput()
	if err != nil {
		outStr := strings.TrimSpace(string(out))
		if strings.Contains(outStr, "No destinations added") || strings.Contains(outStr, "Unknown") {
			return QueueInfo{}, fmt.Errorf("impresora '%s' no encontrada en CUPS", printerName)
		}
		return QueueInfo{
			PrinterName: printerName,
			JobsCount:   0,
			Status:      "idle",
		}, nil
	}

	var jobs []PrintJob
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		jobID := 0
		parts := strings.SplitN(fields[0], "-", 2)
		if len(parts) == 2 {
			fmt.Sscanf(parts[1], "%d", &jobID)
		}
		docName := strings.Join(fields[2:len(fields)-1], " ")
		jobs = append(jobs, PrintJob{
			ID:           jobID,
			DocumentName: docName,
			State:        "pending",
		})
	}

	status := "idle"
	if len(jobs) > 0 {
		status = "processing"
	}

	return QueueInfo{
		PrinterName: printerName,
		JobsCount:   len(jobs),
		Status:      status,
		Jobs:        jobs,
	}, nil
}

const launchAgentLabel = "com.cronos.pos-agent"

func launchAgentPlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")
}

func isAutostartEnabled() bool {
	_, err := os.Stat(launchAgentPlistPath())
	return err == nil
}

func enableAutostart() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("no se pudo obtener la ruta del ejecutable: %w", err)
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <false/>
</dict>
</plist>`, launchAgentLabel, exePath)

	return os.WriteFile(launchAgentPlistPath(), []byte(plist), 0644)
}

func disableAutostart() error {
	return os.Remove(launchAgentPlistPath())
}
