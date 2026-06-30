//go:build windows

package main

import (
	"fmt"
	"os"

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
