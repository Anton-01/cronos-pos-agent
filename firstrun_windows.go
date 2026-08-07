//go:build windows

package main

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

// Ventana nativa de bienvenida (Win32).
//
// Se construye directamente sobre user32/gdi32 con syscall, igual que ya hace
// printer_windows.go con ShellExecuteW, y no con un toolkit gráfico:
//
//   - github.com/lxn/win está sin mantenimiento desde 2021 y arrastraría el
//     mismo trabajo (registrar la clase, el bucle de mensajes, pintar el
//     bitmap) sólo que con otra fachada.
//   - fyne necesita CGO y OpenGL, y añade decenas de MB a un binario que hoy
//     ocupa unos pocos: un precio desproporcionado para una ventana que se
//     abre una vez en la vida del equipo.
//   - una MessageBox nativa no admite imagen propia, y el requisito es mostrar
//     la ilustración del gato.
//
// Cero dependencias nuevas y el ejecutable sigue siendo autónomo.

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassEx  = user32.NewProc("RegisterClassExW")
	procCreateWindowEx   = user32.NewProc("CreateWindowExW")
	procDefWindowProc    = user32.NewProc("DefWindowProcW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procShowWindow       = user32.NewProc("ShowWindow")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procGetMessage       = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessage  = user32.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procLoadCursor       = user32.NewProc("LoadCursorW")
	procLoadImage        = user32.NewProc("LoadImageW")
	procBeginPaint       = user32.NewProc("BeginPaint")
	procEndPaint         = user32.NewProc("EndPaint")
	procDrawText         = user32.NewProc("DrawTextW")
	procSendMessage      = user32.NewProc("SendMessageW")
	procAdjustWindowRect = user32.NewProc("AdjustWindowRect")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procSetForeground    = user32.NewProc("SetForegroundWindow")

	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procCreateFont       = gdi32.NewProc("CreateFontW")
	procSelectObject     = gdi32.NewProc("SelectObject")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
	procSetBkMode        = gdi32.NewProc("SetBkMode")
	procSetTextColor     = gdi32.NewProc("SetTextColor")
	procStretchDIBits    = gdi32.NewProc("StretchDIBits")
	procSetStretchMode   = gdi32.NewProc("SetStretchBltMode")
	procSetBrushOrgEx    = gdi32.NewProc("SetBrushOrgEx")

	procGetModuleHandle = kernel32.NewProc("GetModuleHandleW")
)

// Constantes de la API de Windows usadas por la ventana.
const (
	wsOverlapped    = 0x00000000
	wsCaption       = 0x00C00000
	wsSysMenu       = 0x00080000
	wsMinimizeBox   = 0x00020000
	wsChild         = 0x40000000
	wsVisible       = 0x10000000
	wsTabStop       = 0x00010000
	bsDefPushButton = 0x00000001

	wmDestroy = 0x0002
	wmClose   = 0x0010
	wmPaint   = 0x000F
	wmCommand = 0x0111
	wmSetFont = 0x0030

	swShow = 5

	idcArrow       = 32512
	imageIcon      = 1
	lrLoadFromFile = 0x00000010
	lrDefaultSize  = 0x00000040

	dtCenter     = 0x00000001
	dtVCenter    = 0x00000004
	dtSingleLine = 0x00000020
	dtWordBreak  = 0x00000010

	transparentBkMode = 1
	halftone          = 4
	dibRGBColors      = 0
	srcCopy           = 0x00CC0020

	smCXScreen = 0
	smCYScreen = 1

	idCloseButton = 1
)

// Geometría del área cliente, en píxeles lógicos.
const (
	clientWidth  = 468
	clientHeight = 384

	imageX = 14
	imageY = 14
	imageW = 440
	imageH = 220
)

const (
	welcomeTitle    = "¡Cronos POS se ha instalado correctamente!"
	welcomeSubtitle = "El agente ya está funcionando y aparece como un gato en la bandeja del sistema, junto al reloj. Se iniciará solo cada vez que encienda el equipo."
	welcomeCaption  = "Cronos POS Agent"
	closeButtonText = "✕   Cerrar"
)

type win32Rect struct {
	left, top, right, bottom int32
}

type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   syscall.Handle
	icon       syscall.Handle
	cursor     syscall.Handle
	background syscall.Handle
	menuName   *uint16
	className  *uint16
	iconSm     syscall.Handle
}

type win32Point struct{ x, y int32 }

type win32Msg struct {
	hwnd    syscall.Handle
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      win32Point
}

type paintStruct struct {
	hdc         syscall.Handle
	fErase      int32
	rcPaint     win32Rect
	fRestore    int32
	fIncUpdate  int32
	rgbReserved [32]byte
}

type bitmapInfoHeader struct {
	size          uint32
	width         int32
	height        int32
	planes        uint16
	bitCount      uint16
	compression   uint32
	sizeImage     uint32
	xPelsPerMeter int32
	yPelsPerMeter int32
	clrUsed       uint32
	clrImportant  uint32
}

// deviceImage es un bitmap listo para StretchDIBits: BGRX, filas de arriba
// abajo (altura negativa en la cabecera).
type deviceImage struct {
	header bitmapInfoHeader
	pixels []byte
	width  int32
	height int32
}

// welcomeWindow agrupa el estado que necesita el procedimiento de ventana.
// Sólo se abre una ventana por proceso, así que vive en una variable global:
// el WndProc es una callback de C y no recibe contexto propio.
var welcomeState struct {
	image      *deviceImage
	titleFont  syscall.Handle
	bodyFont   syscall.Handle
	buttonFont syscall.Handle
}

// showWelcomeWindow crea la ventana, la muestra y bloquea hasta que el usuario
// la cierra (con el botón "Cerrar", con la X del título o con Alt+F4).
func showWelcomeWindow() error {
	// El bucle de mensajes de Win32 es por hilo: la ventana debe crearse y
	// bombearse siempre desde el mismo hilo del sistema operativo, y el
	// planificador de Go podría mover la goroutine a otro.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	instance, _, _ := procGetModuleHandle.Call(0)

	img, err := decodeDeviceImage(welcomeCatPNG)
	if err != nil {
		return fmt.Errorf("no se pudo decodificar la ilustración: %w", err)
	}
	welcomeState.image = img

	welcomeState.titleFont = createFont(-20, 700)
	welcomeState.bodyFont = createFont(-13, 400)
	welcomeState.buttonFont = createFont(-14, 600)
	defer func() {
		for _, font := range []syscall.Handle{welcomeState.titleFont, welcomeState.bodyFont, welcomeState.buttonFont} {
			if font != 0 {
				procDeleteObject.Call(uintptr(font))
			}
		}
	}()

	className, err := syscall.UTF16PtrFromString("CronosWelcomeWindow")
	if err != nil {
		return err
	}

	background, _, _ := procCreateSolidBrush.Call(0x00FFFFFF)
	icon := loadEmbeddedIcon()

	class := wndClassEx{
		style:      0x0003, // CS_HREDRAW | CS_VREDRAW
		wndProc:    syscall.NewCallback(welcomeWndProc),
		instance:   syscall.Handle(instance),
		icon:       icon,
		iconSm:     icon,
		background: syscall.Handle(background),
		className:  className,
	}
	class.size = uint32(unsafe.Sizeof(class))
	cursor, _, _ := procLoadCursor.Call(0, uintptr(idcArrow))
	class.cursor = syscall.Handle(cursor)

	// Un error de registro por "la clase ya existe" no es un problema: se
	// reutiliza la registrada antes. Cualquier otro fallo se detecta al crear
	// la ventana, que es lo que realmente importa.
	procRegisterClassEx.Call(uintptr(unsafe.Pointer(&class)))

	style := uintptr(wsOverlapped | wsCaption | wsSysMenu | wsMinimizeBox)

	// El tamaño de CreateWindowEx incluye el marco y la barra de título:
	// AdjustWindowRect lo traduce desde el área cliente que se quiere pintar.
	rect := win32Rect{0, 0, clientWidth, clientHeight}
	procAdjustWindowRect.Call(uintptr(unsafe.Pointer(&rect)), style, 0)
	windowW := rect.right - rect.left
	windowH := rect.bottom - rect.top

	screenW, _, _ := procGetSystemMetrics.Call(smCXScreen)
	screenH, _, _ := procGetSystemMetrics.Call(smCYScreen)
	posX := (int32(screenW) - windowW) / 2
	posY := (int32(screenH) - windowH) / 2

	caption, err := syscall.UTF16PtrFromString(welcomeCaption)
	if err != nil {
		return err
	}

	hwnd, _, callErr := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(caption)),
		style,
		uintptr(posX),
		uintptr(posY),
		uintptr(windowW),
		uintptr(windowH),
		0,
		0,
		instance,
		0,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW falló: %v", callErr)
	}

	if err := createCloseButton(syscall.Handle(hwnd), instance); err != nil {
		return err
	}

	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)
	procSetForeground.Call(hwnd) // por delante de la ventana del instalador

	var msg win32Msg
	for {
		ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 { // 0 = WM_QUIT, -1 = error
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}

	return nil
}

// createCloseButton añade el botón "Cerrar" con el aspa clásica de cerrar
// ventana. La ventana conserva además la X estándar de la barra de título.
func createCloseButton(parent syscall.Handle, instance uintptr) error {
	buttonClass, err := syscall.UTF16PtrFromString("BUTTON")
	if err != nil {
		return err
	}
	label, err := syscall.UTF16PtrFromString(closeButtonText)
	if err != nil {
		return err
	}

	const (
		buttonW = 136
		buttonH = 36
	)

	button, _, callErr := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(label)),
		uintptr(wsChild|wsVisible|wsTabStop|bsDefPushButton),
		uintptr((clientWidth-buttonW)/2),
		uintptr(clientHeight-buttonH-16),
		buttonW,
		buttonH,
		uintptr(parent),
		idCloseButton,
		instance,
		0,
	)
	if button == 0 {
		return fmt.Errorf("no se pudo crear el botón Cerrar: %v", callErr)
	}

	procSendMessage.Call(button, wmSetFont, uintptr(welcomeState.buttonFont), 1)
	return nil
}

// welcomeWndProc es el procedimiento de ventana: pinta el contenido y cierra.
func welcomeWndProc(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmPaint:
		paintWelcome(hwnd)
		return 0

	case wmCommand:
		if uint16(wParam) == idCloseButton {
			procDestroyWindow.Call(uintptr(hwnd))
			return 0
		}

	case wmClose:
		procDestroyWindow.Call(uintptr(hwnd))
		return 0

	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProc.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return ret
}

func paintWelcome(hwnd syscall.Handle) {
	var ps paintStruct
	hdcRet, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	hdc := hdcRet
	defer procEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))

	drawDeviceImage(hdc, welcomeState.image, imageX, imageY, imageW, imageH)

	procSetBkMode.Call(hdc, transparentBkMode)

	drawText(hdc, welcomeTitle, welcomeState.titleFont, 0x00201510, // BGR: azul muy oscuro
		win32Rect{24, 250, clientWidth - 24, 284}, dtCenter|dtSingleLine|dtVCenter)

	drawText(hdc, welcomeSubtitle, welcomeState.bodyFont, 0x00706050,
		win32Rect{34, 288, clientWidth - 34, 336}, dtCenter|dtWordBreak)
}

func drawText(hdc uintptr, text string, font syscall.Handle, color uint32, rect win32Rect, format uint32) {
	utf16, err := syscall.UTF16FromString(text)
	if err != nil {
		return
	}

	previous, _, _ := procSelectObject.Call(hdc, uintptr(font))
	procSetTextColor.Call(hdc, uintptr(color))
	procDrawText.Call(
		hdc,
		uintptr(unsafe.Pointer(&utf16[0])),
		uintptr(len(utf16)-1), // sin el NUL final
		uintptr(unsafe.Pointer(&rect)),
		uintptr(format),
	)
	procSelectObject.Call(hdc, previous)
}

func createFont(height, weight int32) syscall.Handle {
	name, err := syscall.UTF16PtrFromString("Segoe UI")
	if err != nil {
		return 0
	}
	font, _, _ := procCreateFont.Call(
		uintptr(height), // altura negativa = alto de carácter, no de celda
		0, 0, 0,
		uintptr(weight),
		0, 0, 0,
		1, // DEFAULT_CHARSET
		0, 0,
		5, // CLEARTYPE_QUALITY
		0,
		uintptr(unsafe.Pointer(name)),
	)
	return syscall.Handle(font)
}

// drawDeviceImage vuelca el bitmap escalándolo al rectángulo indicado. La
// ilustración se guarda al doble de tamaño, así que aquí siempre se reduce:
// con HALFTONE la reducción promedia píxeles en lugar de descartarlos.
func drawDeviceImage(hdc uintptr, img *deviceImage, x, y, w, h int32) {
	if img == nil {
		return
	}

	procSetStretchMode.Call(hdc, halftone)
	procSetBrushOrgEx.Call(hdc, 0, 0, 0) // requerido tras SetStretchBltMode(HALFTONE)

	procStretchDIBits.Call(
		hdc,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		0, 0, uintptr(img.width), uintptr(img.height),
		uintptr(unsafe.Pointer(&img.pixels[0])),
		uintptr(unsafe.Pointer(&img.header)),
		dibRGBColors,
		srcCopy,
	)
}

// decodeDeviceImage convierte el PNG embebido en un DIB de 32 bits.
//
// StretchDIBits ignora el canal alfa de un DIB BI_RGB, así que la
// transparencia se compone aquí mismo contra el blanco del fondo de la ventana;
// de lo contrario los bordes suavizados de la ilustración saldrían con un halo.
func decodeDeviceImage(data []byte) (*deviceImage, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("imagen vacía")
	}

	pixels := make([]byte, w*h*4)
	i := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			// RGBA() devuelve los canales premultiplicados por alfa en 16 bits.
			r, g, b, a := img.At(x, y).RGBA()
			inv := 0xFFFF - a
			pixels[i+0] = byte((b + inv) >> 8) // sobre blanco: + inv * 0xFFFF/0xFFFF
			pixels[i+1] = byte((g + inv) >> 8)
			pixels[i+2] = byte((r + inv) >> 8)
			pixels[i+3] = 0
			i += 4
		}
	}

	out := &deviceImage{pixels: pixels, width: int32(w), height: int32(h)}
	out.header = bitmapInfoHeader{
		size:      uint32(unsafe.Sizeof(out.header)),
		width:     int32(w),
		height:    -int32(h), // negativo = filas de arriba abajo
		planes:    1,
		bitCount:  32,
		sizeImage: uint32(len(pixels)),
	}
	return out, nil
}

// loadEmbeddedIcon materializa el .ico embebido en un archivo temporal y lo
// carga con LoadImageW: la API de Windows sólo sabe leer iconos desde un
// recurso del ejecutable o desde un archivo, nunca desde memoria.
//
// Se usa el archivo y no el recurso Win32 de rsrc_windows_amd64.syso porque
// LoadIcon exige conocer el identificador que le asignó el compilador de
// recursos; los bytes de //go:embed son la misma imagen y no dependen de la
// cadena de build. Si la escritura falla, la ventana simplemente sale con el
// icono por defecto.
func loadEmbeddedIcon() syscall.Handle {
	path := filepath.Join(os.TempDir(), "cronos-app-icon.ico")

	if info, err := os.Stat(path); err != nil || info.Size() != int64(len(appIconICO)) {
		if err := os.WriteFile(path, appIconICO, 0o644); err != nil {
			return 0
		}
	}

	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}

	icon, _, _ := procLoadImage.Call(
		0,
		uintptr(unsafe.Pointer(pathPtr)),
		imageIcon,
		0, 0, // 0,0 con LR_DEFAULTSIZE = tamaño estándar del sistema
		lrLoadFromFile|lrDefaultSize,
	)
	return syscall.Handle(icon)
}
