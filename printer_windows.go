//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/alexbrainman/printer"
	"golang.org/x/sys/windows/registry"
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

func queryPrintQueue(printerName string) (QueueInfo, error) {
	psCmd := fmt.Sprintf(
		"Get-PrintJob -PrinterName '%s' | Select-Object Id, DocumentName, @{Name='JobState';Expression={$_.JobStatus}} | ConvertTo-Json -Compress",
		printerName,
	)

	out, err := exec.Command("powershell", "-NoProfile", "-Command", psCmd).CombinedOutput()
	if err != nil {
		return QueueInfo{}, fmt.Errorf("error consultando cola de impresión: %w (salida: %s)", err, string(out))
	}

	trimmed := string(out)
	if trimmed == "" || trimmed == "\r\n" || trimmed == "\n" {
		return QueueInfo{
			PrinterName: printerName,
			JobsCount:   0,
			Status:      "idle",
		}, nil
	}

	type psJob struct {
		Id           int    `json:"Id"`
		DocumentName string `json:"DocumentName"`
		JobState     string `json:"JobState"`
	}

	var jobs []psJob

	if trimmed[0] == '[' {
		if err := json.Unmarshal([]byte(trimmed), &jobs); err != nil {
			return QueueInfo{}, fmt.Errorf("error parseando JSON de cola: %w", err)
		}
	} else {
		var single psJob
		if err := json.Unmarshal([]byte(trimmed), &single); err != nil {
			return QueueInfo{}, fmt.Errorf("error parseando JSON de cola: %w", err)
		}
		jobs = append(jobs, single)
	}

	result := QueueInfo{
		PrinterName: printerName,
		JobsCount:   len(jobs),
		Status:      "processing",
	}

	for _, j := range jobs {
		result.Jobs = append(result.Jobs, PrintJob{
			ID:           j.Id,
			DocumentName: j.DocumentName,
			State:        j.JobState,
		})
	}

	if len(jobs) == 0 {
		result.Status = "idle"
	}

	return result, nil
}

const registryKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
const registryValueName = "CronosPOSAgent"

func isAutostartEnabled() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, registryKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()

	_, _, err = key.GetStringValue(registryValueName)
	return err == nil
}

func enableAutostart() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("no se pudo obtener la ruta del ejecutable: %w", err)
	}

	key, _, err := registry.CreateKey(registry.CURRENT_USER, registryKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("no se pudo abrir el registro de Windows: %w", err)
	}
	defer key.Close()

	if err := key.SetStringValue(registryValueName, exePath); err != nil {
		return fmt.Errorf("no se pudo escribir en el registro: %w", err)
	}
	return nil
}

func killOrphanInstances() {
	currentPID := os.Getpid()

	out, _ := exec.Command("tasklist", "/FI", "IMAGENAME eq cronos-pos-agent.exe", "/FO", "CSV", "/NH").CombinedOutput()

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "No tasks") {
			continue
		}
		fields := strings.Split(line, "\",\"")
		if len(fields) < 2 {
			continue
		}
		pidStr := strings.Trim(fields[1], "\"")
		pid, err := strconv.Atoi(pidStr)
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

func disableAutostart() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, registryKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("no se pudo abrir el registro de Windows: %w", err)
	}
	defer key.Close()

	if err := key.DeleteValue(registryValueName); err != nil {
		return fmt.Errorf("no se pudo eliminar la clave del registro: %w", err)
	}
	return nil
}
