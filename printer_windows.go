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
	"syscall"
	"unsafe"

	"github.com/alexbrainman/printer"
	"golang.org/x/sys/windows/registry"
)

// createNoWindow es la bandera CREATE_NO_WINDOW de la API de Windows. Evita que
// cualquier subproceso (PowerShell, tasklist, etc.) levante una ventana de
// consola visible que provoque parpadeos en pantalla.
const createNoWindow = 0x08000000

// detachedProcess es la bandera DETACHED_PROCESS: el hijo relanzado desde la
// ruta permanente sobrevive a la muerte del proceso que lo lanzó.
const detachedProcess = 0x00000008

// hiddenCommand construye un *exec.Cmd con SysProcAttr configurado para ejecutar
// el subproceso de forma totalmente oculta (sin ventana de consola). Se usa en
// TODAS las invocaciones nativas de Windows para garantizar arranque silencioso.
func hiddenCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	return cmd
}

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

// rawPrint sends an ESC/POS ticket to the Windows spooler. Before a single byte
// is written, BuildESCPOSPayload prepares the payload so that the print stream
// ALWAYS opens with the same two commands:
//
//	1B 40      ESC @    -> reset the printer to a known state
//	1B 74 10   ESC t 16 -> Code Page 1252 (Windows Latin-1), the default
//	                       since v1.5.0
//
// and then transcodes the UTF-8 text to the bytes of that very code page.
// Without both halves the printer decodes every UTF-8 byte on its own against
// whatever table it has active, and the accented capitals come out as loose
// symbols: the "†nimo" failure instead of "Ánimo".
//
// The order matters: "ESC @" restores the factory code page, so the selection
// goes behind it — including behind an "ESC @" the payload brings itself, in
// which case no second reset is injected.
func rawPrint(printerName string, data []byte, enc EncodingOptions) error {
	payload, err := BuildESCPOSPayload(data, enc)
	if err != nil {
		return err
	}

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

	if _, err := p.Write(payload); err != nil {
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

	verbPtr, _ := syscall.UTF16PtrFromString("print")
	filePtr, _ := syscall.UTF16PtrFromString(tmpFile.Name())
	printerParam := fmt.Sprintf(`/p /h "%s"`, printerName)
	paramsPtr, _ := syscall.UTF16PtrFromString(printerParam)

	shell32 := syscall.NewLazyDLL("shell32.dll")
	shellExecute := shell32.NewProc("ShellExecuteW")

	ret, _, _ := shellExecute.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(filePtr)),
		uintptr(unsafe.Pointer(paramsPtr)),
		0,
		0, // SW_HIDE
	)

	if ret <= 32 {
		return fmt.Errorf("ShellExecuteW falló con código %d para impresora '%s'", ret, printerName)
	}

	return nil
}

func queryPrintQueue(printerName string) (QueueInfo, error) {
	psCmd := fmt.Sprintf(
		"Get-PrintJob -PrinterName '%s' | Select-Object Id, DocumentName, @{Name='JobState';Expression={$_.JobStatus}} | ConvertTo-Json -Compress",
		printerName,
	)

	out, err := hiddenCommand("powershell", "-NoProfile", "-Command", psCmd).CombinedOutput()
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

// autostartTargetPath devuelve la ruta que debe quedar escrita en el registro.
// Siempre se prefiere la ubicación permanente del binario: si se registrara la
// ruta del proceso en curso (Descargas, %TEMP%, un pendrive…), la entrada
// quedaría apuntando a un archivo que desaparece y el auto-arranque fallaría
// en el siguiente reinicio, que es justamente el fallo que se corrige.
func autostartTargetPath() (string, error) {
	exe, err := currentExePath()
	if err == nil && isPermanentLocation(exe) {
		return exe, nil
	}
	if installed, ok := installedExePath(); ok {
		return installed, nil
	}
	if err != nil {
		return "", fmt.Errorf("no se pudo obtener la ruta del ejecutable: %w", err)
	}
	return exe, nil
}

// quotedRegistryPath encierra la ruta entre comillas dobles, tal y como exige
// el registro de Windows: sin ellas el shell trunca el comando en el primer
// espacio (`C:\Program Files\...` se lee como `C:\Program`) y la entrada se
// ignora silenciosamente tras un reinicio o una actualización del sistema.
func quotedRegistryPath(exePath string) string {
	return fmt.Sprintf(`"%s"`, exePath)
}

func enableAutostart() error {
	exePath, err := autostartTargetPath()
	if err != nil {
		return err
	}

	key, _, err := registry.CreateKey(registry.CURRENT_USER, registryKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("no se pudo abrir el registro de Windows: %w", err)
	}
	defer key.Close()

	if err := key.SetStringValue(registryValueName, quotedRegistryPath(exePath)); err != nil {
		return fmt.Errorf("no se pudo escribir en el registro: %w", err)
	}
	return nil
}

// EnsureAutostartRegistered repara la entrada de auto-arranque en cada arranque
// del agente. Se ejecuta siempre porque el valor del registro puede haber
// quedado obsoleto: apuntando a una carpeta ya borrada, a una versión antigua
// instalada en otra ruta, o escrito sin comillas por un instalador anterior.
//
// Respeta la preferencia del usuario: si desmarcó "Iniciar con el Sistema" en
// el systray, la entrada no se vuelve a crear.
func EnsureAutostartRegistered() {
	if !AutostartPreferred() {
		return
	}

	exePath, err := autostartTargetPath()
	if err != nil {
		log.Printf("[autostart] No se pudo determinar la ruta a registrar: %v", err)
		return
	}
	expected := quotedRegistryPath(exePath)

	key, err := registry.OpenKey(registry.CURRENT_USER, registryKeyPath, registry.QUERY_VALUE)
	if err == nil {
		current, _, readErr := key.GetStringValue(registryValueName)
		key.Close()
		if readErr == nil && strings.EqualFold(strings.TrimSpace(current), expected) {
			return // ya apunta a la ruta correcta y entrecomillada
		}
		if readErr == nil {
			log.Printf("[autostart] Entrada obsoleta (%s), se reescribe a %s", current, expected)
		}
	}

	if err := enableAutostart(); err != nil {
		log.Printf("[autostart] No se pudo registrar el auto-arranque: %v", err)
		return
	}
	// El nombre del usuario no es adorno: la entrada vive en HKCU, que es la
	// rama del usuario que ejecuta el agente. Si alguien lanzó el .exe con
	// "Ejecutar como administrador", queda registrada en la rama del
	// administrador y el agente no vuelve tras reiniciar en la sesión del
	// operador. Con el usuario en el log, ese caso se ve de un vistazo en vez
	// de deducirse.
	log.Printf("[autostart] Auto-arranque registrado como %s (usuario %s, HKCU\\%s)",
		expected, os.Getenv("USERNAME"), registryKeyPath)
}

func killOrphanInstances() {
	currentPID := os.Getpid()

	out, _ := hiddenCommand("tasklist", "/FI", "IMAGENAME eq cronos-pos-agent.exe", "/FO", "CSV", "/NH").CombinedOutput()

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

// printerRegistryRoot is where the Windows spooler keeps the per-printer
// settings. The port is read from here because the Win32 call that would return
// it (EnumPrinters with PRINTER_INFO_2) is not exposed by the printer package,
// and the port is the single most useful field when a till "does not print":
// USB001, a COM port and a \\server\queue share fail in completely different
// ways.
const printerRegistryRoot = `SYSTEM\CurrentControlSet\Control\Print\Printers`

// describePrinter collects what Windows knows about a printer for the self-test
// ticket.
//
// Every lookup is best-effort and independent: a driver that cannot be read does
// not cost the port, and neither costs the ticket. What fails is recorded in
// Notes and printed on the paper, because "no se pudo leer el controlador" is
// itself a diagnosis — it usually means the queue exists but its driver package
// is broken, which is exactly the kind of thing nobody can describe by phone.
func describePrinter(name string) PrinterTechnicalInfo {
	info := PrinterTechnicalInfo{Name: name}

	if def, err := printer.Default(); err == nil {
		info.IsDefault = strings.EqualFold(strings.TrimSpace(def), strings.TrimSpace(name))
	} else {
		info.Notes = append(info.Notes, "no se pudo leer la impresora predeterminada")
	}

	p, err := printer.Open(name)
	if err != nil {
		info.Notes = append(info.Notes, "no se pudo abrir la cola: "+err.Error())
	} else {
		defer p.Close()

		if driver, err := p.DriverInfo(); err == nil {
			info.Driver = driver.Name
			info.Processor = driver.Environment
		} else {
			info.Notes = append(info.Notes, "no se pudo leer el controlador")
		}

		// Native EnumJobs, not the PowerShell of queryPrintQueue: the test
		// ticket must print on a till whose PowerShell execution policy blocks
		// scripts, and it must not take a second to do it.
		if jobs, err := p.Jobs(); err == nil {
			info.QueuedIDs = len(jobs)
		}
	}

	if port, err := printerPortFromRegistry(name); err == nil {
		info.Port = port
	} else {
		info.Notes = append(info.Notes, "no se pudo leer el puerto")
	}

	return info
}

// printerPortFromRegistry reads the "Port" value the spooler stores for a
// printer (USB001, COM3, \\host\queue, …).
func printerPortFromRegistry(name string) (string, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, printerRegistryRoot+`\`+name, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer key.Close()

	port, _, err := key.GetStringValue("Port")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(port), nil
}
