//go:build windows

package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	// installFolderName es la carpeta fija del agente dentro de la raíz
	// permanente elegida (Program Files, ProgramData o LocalAppData).
	installFolderName = "CronosAgent"

	// agentExeName es el nombre obligatorio del binario instalado. Debe
	// coincidir con el filtro de tasklist en killOrphanInstances().
	agentExeName = "cronos-pos-agent.exe"
)

// permanentRoots lista, en orden de preferencia, las raíces donde el binario
// puede vivir de forma permanente:
//
//  1. %ProgramFiles%  — ubicación estándar de una aplicación instalada (el
//     modelo de QZ Tray). Requiere privilegios de administrador para escribir,
//     que es exactamente lo que la hace inmune a borrados accidentales.
//  2. %ProgramData%   — permanente y compartida entre usuarios, escribible sin
//     elevación (el usuario que crea la carpeta queda como propietario).
//  3. %LOCALAPPDATA%  — último recurso por usuario, siempre escribible.
//
// Nunca se instala en Descargas, Escritorio ni %TEMP%: son precisamente las
// rutas volátiles que rompían el auto-arranque tras un reinicio o una limpieza
// de disco, porque la entrada del registro quedaba apuntando a un archivo
// inexistente.
func permanentRoots() []string {
	var roots []string
	for _, env := range []string{"ProgramFiles", "ProgramData", "LOCALAPPDATA"} {
		if dir := os.Getenv(env); dir != "" {
			roots = append(roots, filepath.Join(dir, installFolderName))
		}
	}
	return roots
}

// permanentExePaths devuelve las rutas completas candidatas del binario.
func permanentExePaths() []string {
	roots := permanentRoots()
	paths := make([]string, 0, len(roots))
	for _, root := range roots {
		paths = append(paths, filepath.Join(root, agentExeName))
	}
	return paths
}

// currentExePath devuelve la ruta absoluta y limpia del ejecutable en curso.
func currentExePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Clean(exe), nil
}

// samePath compara rutas de Windows sin distinguir mayúsculas ni separadores.
func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// isPermanentLocation indica si la ruta dada es una de las ubicaciones fijas.
// Se aceptan todas las candidatas (no sólo la preferida) para que un agente ya
// instalado en ProgramData no se reubique a Program Files en cada arranque.
func isPermanentLocation(path string) bool {
	for _, candidate := range permanentExePaths() {
		if samePath(path, candidate) {
			return true
		}
	}
	return false
}

// installedExePath busca el binario ya instalado en alguna ruta permanente.
func installedExePath() (string, bool) {
	for _, candidate := range permanentExePaths() {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

// writableInstallTarget elige la primera raíz permanente en la que este proceso
// puede escribir realmente, comprobándolo con un archivo de prueba (crear la
// carpeta no garantiza permiso de escritura dentro de ella).
func writableInstallTarget() (string, error) {
	var lastErr error

	for _, root := range permanentRoots() {
		if err := os.MkdirAll(root, 0o755); err != nil {
			lastErr = err
			continue
		}

		probe, err := os.CreateTemp(root, ".cronos-write-test-*")
		if err != nil {
			lastErr = err
			continue
		}
		probe.Close()
		os.Remove(probe.Name())

		return filepath.Join(root, agentExeName), nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no se encontró ninguna raíz de instalación")
	}
	return "", lastErr
}

// agentDir devuelve el directorio de DATOS del agente: config.json, logs y
// certificados. Se separa del directorio del binario porque la ubicación
// permanente (Program Files) es de sólo lectura para un usuario estándar,
// mientras que el agente necesita escribir su configuración en cada arranque.
//
// Coincide con la carpeta que usaban las versiones anteriores instaladas en
// %LOCALAPPDATA%\CronosAgent, de modo que el token de seguridad ya emitido al
// frontend sobrevive a la actualización.
func agentDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = os.Getenv("APPDATA")
	}
	if base == "" {
		// Sin variables de entorno de perfil, se cae al directorio del binario.
		if exe, err := currentExePath(); err == nil {
			return filepath.Dir(exe)
		}
		return "."
	}

	dir := filepath.Join(base, installFolderName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		if exe, err := currentExePath(); err == nil {
			return filepath.Dir(exe)
		}
		return "."
	}
	return dir
}

// runtimeFileNames son los archivos generados en runtime que deben acompañar al
// agente cuando su binario se reubica a la ruta permanente.
var runtimeFileNames = []string{
	configFileName,
	keyFileName,
	certFileName,
}

// migrateRuntimeData copia config.json, la llave y el certificado desde una
// instalación anterior al directorio de datos actual. Es crítico para no
// regenerar el api_token: el frontend ya lo tiene guardado y perderlo obligaría
// a reconfigurar cada caja de cobro.
func migrateRuntimeData(fromDir string) {
	target := agentDir()
	if samePath(fromDir, target) {
		return
	}

	for _, name := range runtimeFileNames {
		src := filepath.Join(fromDir, name)
		dst := filepath.Join(target, name)

		if _, err := os.Stat(dst); err == nil {
			continue // ya existe en el destino: no se pisa
		}
		if _, err := os.Stat(src); err != nil {
			continue // no existe en el origen: nada que migrar
		}
		if err := copyFile(src, dst); err != nil {
			log.Printf("[install] No se pudo migrar '%s': %v", name, err)
			continue
		}
		log.Printf("[install] Migrado '%s' desde %s", name, fromDir)
	}
}

// EnsurePermanentLocation garantiza que el agente se ejecute desde su ruta fija
// y permanente, replicando el comportamiento de QZ Tray: si el binario se lanzó
// desde Descargas, el Escritorio o una carpeta temporal, se copia a la ruta
// definitiva, se migran los datos de runtime y se relanza desde allí.
//
// Devuelve true cuando ha relanzado el agente: el proceso actual debe terminar
// para que no queden dos instancias compitiendo por el puerto.
func EnsurePermanentLocation() bool {
	exe, err := currentExePath()
	if err != nil {
		log.Printf("[install] No se pudo determinar la ruta del ejecutable: %v", err)
		return false
	}

	if isPermanentLocation(exe) {
		return false
	}

	target, err := writableInstallTarget()
	if err != nil {
		log.Printf("[install] Sin ubicación permanente escribible, se continúa desde '%s': %v", exe, err)
		return false
	}

	log.Printf("[install] Reubicando el agente de '%s' a '%s'", exe, target)

	if err := replaceExecutable(exe, target); err != nil {
		log.Printf("[install] No se pudo copiar el binario a la ruta permanente: %v", err)
		return false
	}

	// Los datos viven en agentDir(), pero una instalación antigua pudo dejarlos
	// junto al binario de origen.
	migrateRuntimeData(filepath.Dir(exe))

	if err := relaunchFrom(target); err != nil {
		log.Printf("[install] No se pudo relanzar desde la ruta permanente: %v", err)
		return false
	}

	log.Printf("[install] Agente relanzado desde la ruta permanente '%s'", target)
	return true
}

// replaceExecutable copia el binario en curso sobre la ruta permanente. Windows
// bloquea los ejecutables en uso, así que si la copia directa falla se renombra
// el binario antiguo (operación sí permitida sobre un archivo en ejecución) y se
// reintenta.
func replaceExecutable(src, dst string) error {
	if err := copyFile(src, dst); err == nil {
		return nil
	}

	backup := dst + ".old"
	os.Remove(backup)

	if err := os.Rename(dst, backup); err != nil {
		return fmt.Errorf("destino bloqueado y no renombrable: %w", err)
	}

	if err := copyFile(src, dst); err != nil {
		os.Rename(backup, dst) // se restaura el binario anterior
		return err
	}

	os.Remove(backup) // best-effort: si sigue bloqueado se limpia en el próximo arranque
	return nil
}

// copyFile copia src sobre dst de forma atómica dentro de la carpeta destino:
// escribe un archivo temporal y lo renombra, para no dejar nunca un ejecutable
// a medio escribir si el proceso muere en mitad de la copia.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("error abriendo '%s': %w", src, err)
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("error creando '%s': %w", filepath.Dir(dst), err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".cronos-copy-*")
	if err != nil {
		return fmt.Errorf("error creando archivo temporal en '%s': %w", filepath.Dir(dst), err)
	}
	tmpName := tmp.Name()

	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("error copiando datos: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("error cerrando archivo temporal: %w", err)
	}

	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("error moviendo a '%s': %w", dst, err)
	}
	return nil
}

// relaunchFrom arranca el binario ya instalado en la ruta permanente, sin
// ventana de consola y desligado del proceso actual. El flag --relaunched evita
// cualquier posibilidad de bucle de reubicación.
func relaunchFrom(exePath string) error {
	cmd := exec.Command(exePath, "--relaunched")
	cmd.Dir = filepath.Dir(exePath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow | detachedProcess,
	}
	return cmd.Start()
}
