//go:build darwin

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

// rawPrint sends an ESC/POS ticket to CUPS. As on Windows, BuildESCPOSPayload
// prepares the payload first: it resets the printer ("ESC @"), selects the
// hardware code page ("ESC t n") and transcodes the UTF-8 text to the bytes of
// that page, so that the accented capitals (Á É Í Ó Ú Ñ) print correctly.
func rawPrint(printerName string, data []byte, enc EncodingOptions) error {
	payload, err := BuildESCPOSPayload(data, enc)
	if err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp("", "cronos-ticket-*.bin")
	if err != nil {
		return fmt.Errorf("error creando archivo temporal: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(payload); err != nil {
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

func printPDF(printerName string, pdfBytes []byte) error {
	tmpFile, err := os.CreateTemp("", "cronos-pdf-*.pdf")
	if err != nil {
		return fmt.Errorf("error creando archivo temporal PDF: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(pdfBytes); err != nil {
		tmpFile.Close()
		return fmt.Errorf("error escribiendo PDF al archivo temporal: %w", err)
	}
	tmpFile.Close()

	out, err := exec.Command("lp", "-d", printerName, tmpFile.Name()).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error enviando PDF a impresora '%s': %w (salida: %s)", printerName, err, string(out))
	}

	return nil
}

func killOrphanInstances() {
	currentPID := os.Getpid()

	out, _ := exec.Command("pgrep", "-f", "cronos-pos-agent").CombinedOutput()

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil || pid == currentPID {
			continue
		}

		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		proc.Kill()
		log.Printf("[self-healing] Instancia huérfana PID %d eliminada", pid)
	}
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

	plistPath := launchAgentPlistPath()
	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		return fmt.Errorf("no se pudo crear la carpeta de LaunchAgents: %w", err)
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

	return os.WriteFile(plistPath, []byte(plist), 0644)
}

func disableAutostart() error {
	return os.Remove(launchAgentPlistPath())
}

// describePrinter collects what CUPS knows about a printer for the self-test
// ticket. It mirrors the Windows implementation field by field so that the
// ticket layout does not depend on the platform; what CUPS cannot answer is
// recorded in Notes and printed as such.
func describePrinter(name string) PrinterTechnicalInfo {
	info := PrinterTechnicalInfo{Name: name}

	if out, err := exec.Command("lpstat", "-d").CombinedOutput(); err == nil {
		// "system default destination: EPSON_TM_T20"
		if idx := strings.LastIndex(string(out), ":"); idx >= 0 {
			def := strings.TrimSpace(string(out)[idx+1:])
			info.IsDefault = strings.EqualFold(def, strings.TrimSpace(name))
		}
	} else {
		info.Notes = append(info.Notes, "no se pudo leer la impresora predeterminada")
	}

	// "device for EPSON_TM_T20: usb://EPSON/TM-T20" — the CUPS device URI is
	// the equivalent of the Windows port.
	if out, err := exec.Command("lpstat", "-v", name).CombinedOutput(); err == nil {
		if idx := strings.Index(string(out), ":"); idx >= 0 {
			info.Port = strings.TrimSpace(string(out)[idx+1:])
		}
	} else {
		info.Notes = append(info.Notes, "no se pudo leer el dispositivo")
	}

	if out, err := exec.Command("lpstat", "-l", "-p", name).CombinedOutput(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "Description:") {
				info.Driver = strings.TrimSpace(strings.TrimPrefix(trimmed, "Description:"))
			}
			if strings.HasPrefix(trimmed, "Interface:") {
				info.Processor = strings.TrimSpace(strings.TrimPrefix(trimmed, "Interface:"))
			}
		}
	}

	if queue, err := queryPrintQueue(name); err == nil {
		info.QueuedIDs = queue.JobsCount
	}

	return info
}
