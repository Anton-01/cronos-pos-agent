// Command genassets dibuja y escribe los recursos gráficos del agente:
//
//	app_icon.ico    — icono multi-resolución del System Tray, del ejecutable y
//	                  del instalador (gato tuxedo blanco y negro).
//	app_icon.png    — el mismo gato en PNG, que es el formato que exige
//	                  NSStatusItem en macOS (systray_darwin.m).
//	welcome_cat.png — ilustración de la ventana de bienvenida: el gato tuxedo
//	                  jugando con la ticketera.
//
// Los recursos se generan por código y no se descargan de ninguna parte: así el
// repositorio no depende de binarios opacos y cualquiera puede regenerarlos de
// forma reproducible con:
//
//	go run ./tools/genassets
//
// El resultado se versiona en el repositorio porque el paquete principal lo
// embebe con //go:embed y debe poder compilarse sin ejecutar este generador.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

// -----------------------------------------------------------------------------
// Lienzo con supermuestreo
// -----------------------------------------------------------------------------

// canvas es un lienzo RGBA en coma flotante con canales premultiplicados por
// alfa. Se dibuja a una resolución mayor que la final (supermuestreo) y se
// reduce al tamaño de salida promediando bloques: es la forma más simple de
// obtener bordes suavizados sin implementar un rasterizador analítico.
type canvas struct {
	w, h  int       // dimensiones en píxeles del buffer supermuestreado
	scale float64   // píxeles del buffer por unidad de diseño
	r     []float64 // canales premultiplicados
	g     []float64
	b     []float64
	a     []float64
}

func newCanvas(designW, designH float64, scale float64) *canvas {
	w := int(math.Round(designW * scale))
	h := int(math.Round(designH * scale))
	n := w * h
	return &canvas{
		w: w, h: h, scale: scale,
		r: make([]float64, n),
		g: make([]float64, n),
		b: make([]float64, n),
		a: make([]float64, n),
	}
}

// rgba describe un color en alfa recto (no premultiplicado), en el rango 0–1.
type rgba struct{ r, g, b, a float64 }

func rgb(hex uint32) rgba {
	return rgba{
		r: float64((hex>>16)&0xFF) / 255,
		g: float64((hex>>8)&0xFF) / 255,
		b: float64(hex&0xFF) / 255,
		a: 1,
	}
}

// alpha devuelve el mismo color con la opacidad indicada (sombras, brillos).
func (c rgba) alpha(a float64) rgba { c.a = a; return c }

// over compone un color sobre el píxel indicado con la regla "source over".
func (c *canvas) over(x, y int, col rgba) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h || col.a <= 0 {
		return
	}
	i := y*c.w + x
	inv := 1 - col.a
	c.r[i] = col.r*col.a + c.r[i]*inv
	c.g[i] = col.g*col.a + c.g[i]*inv
	c.b[i] = col.b*col.a + c.b[i]*inv
	c.a[i] = col.a + c.a[i]*inv
}

// fill recorre la caja contenedora (en unidades de diseño) y compone el color
// en los píxeles cuyo centro cae dentro de la forma.
func (c *canvas) fill(minX, minY, maxX, maxY float64, inside func(x, y float64) bool, col rgba) {
	x0 := int(math.Floor(minX * c.scale))
	y0 := int(math.Floor(minY * c.scale))
	x1 := int(math.Ceil(maxX*c.scale)) + 1
	y1 := int(math.Ceil(maxY*c.scale)) + 1

	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > c.w {
		x1 = c.w
	}
	if y1 > c.h {
		y1 = c.h
	}

	for py := y0; py < y1; py++ {
		dy := (float64(py) + 0.5) / c.scale
		for px := x0; px < x1; px++ {
			dx := (float64(px) + 0.5) / c.scale
			if inside(dx, dy) {
				c.over(px, py, col)
			}
		}
	}
}

func (c *canvas) ellipse(cx, cy, rx, ry float64, col rgba) {
	c.fill(cx-rx, cy-ry, cx+rx, cy+ry, func(x, y float64) bool {
		dx := (x - cx) / rx
		dy := (y - cy) / ry
		return dx*dx+dy*dy <= 1
	}, col)
}

func (c *canvas) circle(cx, cy, r float64, col rgba) { c.ellipse(cx, cy, r, r, col) }

func (c *canvas) roundRect(x0, y0, x1, y1, radius float64, col rgba) {
	if radius > (x1-x0)/2 {
		radius = (x1 - x0) / 2
	}
	if radius > (y1-y0)/2 {
		radius = (y1 - y0) / 2
	}
	c.fill(x0, y0, x1, y1, func(x, y float64) bool {
		if x < x0 || x > x1 || y < y0 || y > y1 {
			return false
		}
		cx := math.Min(math.Max(x, x0+radius), x1-radius)
		cy := math.Min(math.Max(y, y0+radius), y1-radius)
		dx, dy := x-cx, y-cy
		return dx*dx+dy*dy <= radius*radius
	}, col)
}

type point struct{ x, y float64 }

// polygon rellena un polígono cerrado con la regla par-impar.
func (c *canvas) polygon(pts []point, col rgba) {
	if len(pts) < 3 {
		return
	}
	minX, minY := pts[0].x, pts[0].y
	maxX, maxY := pts[0].x, pts[0].y
	for _, p := range pts[1:] {
		minX = math.Min(minX, p.x)
		minY = math.Min(minY, p.y)
		maxX = math.Max(maxX, p.x)
		maxY = math.Max(maxY, p.y)
	}

	c.fill(minX, minY, maxX, maxY, func(x, y float64) bool {
		inside := false
		j := len(pts) - 1
		for i := 0; i < len(pts); i++ {
			pi, pj := pts[i], pts[j]
			if (pi.y > y) != (pj.y > y) {
				xCross := pi.x + (y-pi.y)/(pj.y-pi.y)*(pj.x-pi.x)
				if x < xCross {
					inside = !inside
				}
			}
			j = i
		}
		return inside
	}, col)
}

func (c *canvas) triangle(a, b, d point, col rgba) { c.polygon([]point{a, b, d}, col) }

// line traza un segmento de extremos redondeados y grosor width.
func (c *canvas) line(a, b point, width float64, col rgba) {
	h := width / 2
	minX := math.Min(a.x, b.x) - h
	minY := math.Min(a.y, b.y) - h
	maxX := math.Max(a.x, b.x) + h
	maxY := math.Max(a.y, b.y) + h

	vx, vy := b.x-a.x, b.y-a.y
	lenSq := vx*vx + vy*vy

	c.fill(minX, minY, maxX, maxY, func(x, y float64) bool {
		var t float64
		if lenSq > 0 {
			t = ((x-a.x)*vx + (y-a.y)*vy) / lenSq
			t = math.Min(math.Max(t, 0), 1)
		}
		dx := x - (a.x + t*vx)
		dy := y - (a.y + t*vy)
		return dx*dx+dy*dy <= h*h
	}, col)
}

// quadPoint evalúa una curva de Bézier cuadrática en t.
func quadPoint(p0, p1, p2 point, t float64) point {
	u := 1 - t
	return point{
		x: u*u*p0.x + 2*u*t*p1.x + t*t*p2.x,
		y: u*u*p0.y + 2*u*t*p1.y + t*t*p2.y,
	}
}

// quadTangent devuelve la tangente unitaria de la curva en t.
func quadTangent(p0, p1, p2 point, t float64) point {
	u := 1 - t
	dx := 2*u*(p1.x-p0.x) + 2*t*(p2.x-p1.x)
	dy := 2*u*(p1.y-p0.y) + 2*t*(p2.y-p1.y)
	n := math.Hypot(dx, dy)
	if n == 0 {
		return point{1, 0}
	}
	return point{dx / n, dy / n}
}

// quadStroke recorre una Bézier cuadrática con un grosor que interpola de w0 a
// w1 (la cola del gato se estrecha hacia la punta).
func (c *canvas) quadStroke(p0, p1, p2 point, w0, w1 float64, col rgba) {
	const steps = 160
	for i := 0; i <= steps; i++ {
		t := float64(i) / steps
		p := quadPoint(p0, p1, p2, t)
		c.circle(p.x, p.y, (w0+(w1-w0)*t)/2, col)
	}
}

// downsample reduce el buffer supermuestreado al tamaño final promediando
// bloques de factor×factor y deshaciendo la premultiplicación por alfa.
func (c *canvas) downsample(factor int) *image.NRGBA {
	outW, outH := c.w/factor, c.h/factor
	out := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	n := float64(factor * factor)

	for y := 0; y < outH; y++ {
		for x := 0; x < outW; x++ {
			var sr, sg, sb, sa float64
			for dy := 0; dy < factor; dy++ {
				row := (y*factor + dy) * c.w
				for dx := 0; dx < factor; dx++ {
					i := row + x*factor + dx
					sr += c.r[i]
					sg += c.g[i]
					sb += c.b[i]
					sa += c.a[i]
				}
			}
			sr, sg, sb, sa = sr/n, sg/n, sb/n, sa/n

			px := color.NRGBA{A: uint8(math.Round(sa * 255))}
			if sa > 0 {
				px.R = clamp8(sr / sa)
				px.G = clamp8(sg / sa)
				px.B = clamp8(sb / sa)
			}
			out.SetNRGBA(x, y, px)
		}
	}
	return out
}

func clamp8(v float64) uint8 {
	return uint8(math.Round(math.Min(math.Max(v, 0), 1) * 255))
}

// -----------------------------------------------------------------------------
// Paleta
// -----------------------------------------------------------------------------

var (
	furBlack   = rgb(0x1C1C22) // negro suave: el negro puro se ve plano al reducir
	furWhite   = rgb(0xFFFFFF)
	nosePink   = rgb(0xF2A6B3)
	eyeGold    = rgb(0xF5B92E)
	panelBG    = rgb(0xEDF2F9)
	paperWhite = rgb(0xFFFFFF)
	paperEdge  = rgb(0xD5DEEA)
	inkGray    = rgb(0xB4C0D0)
	inkDark    = rgb(0x7C8AA0)
	caseBody   = rgb(0x465071)
	caseFront  = rgb(0x5C6890)
	caseSlot   = rgb(0x272D42)
	ledGreen   = rgb(0x4ADE80)
	shadow     = rgb(0x1C1C22)
)

// -----------------------------------------------------------------------------
// El gato tuxedo
// -----------------------------------------------------------------------------

// catPalette son los colores con los que se dibuja el gato. Cambiarlos es lo
// que convierte el mismo dibujo en los tres iconos de estado del System Tray.
type catPalette struct {
	fur   rgba
	white rgba
	inner rgba // interior de las orejas
	eye   rgba
	nose  rgba
	// badge es el punto de estado de la esquina inferior derecha. Es lo que de
	// verdad se distingue a 16 px: el color del pelaje se pierde, un punto de
	// color saturado no. nil = sin punto.
	badge *rgba
}

// basePalette es el gato "de marca": el del ejecutable, el instalador y la
// ventana de bienvenida.
var basePalette = catPalette{
	fur:   furBlack,
	white: furWhite,
	inner: nosePink,
	eye:   eyeGold,
	nose:  nosePink,
}

// grayPalette es el estado neutro: el agente ha arrancado pero todavía no
// escucha. Todo desaturado, incluidos los ojos, para que se lea "apagado".
var grayPalette = catPalette{
	fur:   rgb(0x8B919B),
	white: rgb(0xE7EAEE),
	inner: rgb(0xB6AEB2),
	eye:   rgb(0xCBD0D7),
	nose:  rgb(0xB6AEB2),
}

// greenPalette es el estado operativo: el gato de marca con un punto verde.
var greenPalette = catPalette{
	fur:   furBlack,
	white: furWhite,
	inner: nosePink,
	eye:   eyeGold,
	nose:  nosePink,
	badge: &statusGreen,
}

var statusGreen = rgb(0x22C55E)

// drawCatFace dibuja la cara del gato tuxedo centrada en (cx, cy) con un radio
// de cabeza r. La usan tanto el icono como la ilustración de bienvenida, de modo
// que el gato de la bandeja y el de la ventana son el mismo personaje.
//
// El patrón "tuxedo" (esmoquin) se consigue con tres manchas blancas sobre el
// negro: la lista de la frente, el hocico y la pechera.
func drawCatFace(c *canvas, cx, cy, r float64, p catPalette) {
	// Nombres locales para no repetir "p." en cada figura del dibujo.
	furBlack, furWhite, eyeGold := p.fur, p.white, p.eye
	earInner, nosePink := p.inner, p.nose

	// Orejas: se dibujan antes que la cabeza para que ésta tape su base.
	leftEar := []point{
		{cx - 0.95*r, cy - 0.28*r},
		{cx - 0.66*r, cy - 1.30*r},
		{cx - 0.08*r, cy - 0.74*r},
	}
	rightEar := mirrorX(leftEar, cx)
	c.polygon(leftEar, furBlack)
	c.polygon(rightEar, furBlack)
	c.polygon(shrink(leftEar, 0.42), earInner)
	c.polygon(shrink(rightEar, 0.42), earInner)

	// Cabeza.
	c.ellipse(cx, cy, r, 0.94*r, furBlack)

	// Lista blanca de la frente, entre los ojos.
	c.polygon([]point{
		{cx - 0.09*r, cy - 0.62*r},
		{cx + 0.09*r, cy - 0.62*r},
		{cx + 0.20*r, cy + 0.35*r},
		{cx - 0.20*r, cy + 0.35*r},
	}, furWhite)

	// Hocico y mentón.
	c.ellipse(cx, cy+0.42*r, 0.60*r, 0.40*r, furWhite)

	// Ojos.
	for _, sign := range []float64{-1, 1} {
		ex := cx + sign*0.42*r
		ey := cy - 0.10*r
		c.ellipse(ex, ey, 0.21*r, 0.25*r, eyeGold)
		c.ellipse(ex, ey, 0.075*r, 0.21*r, furBlack)
		c.circle(ex-0.08*r, ey-0.10*r, 0.055*r, furWhite.alpha(0.9))
	}

	// Nariz y boca.
	c.triangle(
		point{cx - 0.14*r, cy + 0.22*r},
		point{cx + 0.14*r, cy + 0.22*r},
		point{cx, cy + 0.38*r},
		nosePink,
	)
	c.line(point{cx, cy + 0.38*r}, point{cx, cy + 0.52*r}, 0.05*r, furBlack)
	c.line(point{cx, cy + 0.52*r}, point{cx - 0.18*r, cy + 0.58*r}, 0.05*r, furBlack)
	c.line(point{cx, cy + 0.52*r}, point{cx + 0.18*r, cy + 0.58*r}, 0.05*r, furBlack)

	// Bigotes: blancos sobre el negro de las mejillas.
	for _, sign := range []float64{-1, 1} {
		for i, dy := range []float64{-0.06, 0.10, 0.26} {
			from := point{cx + sign*0.52*r, cy + (0.30+dy*0.6)*r}
			to := point{cx + sign*(1.22+0.04*float64(i))*r, cy + dy*r}
			c.line(from, to, 0.035*r, furWhite.alpha(0.85))
		}
	}
}

func mirrorX(pts []point, axis float64) []point {
	out := make([]point, len(pts))
	for i, p := range pts {
		out[i] = point{2*axis - p.x, p.y}
	}
	return out
}

// shrink encoge un polígono hacia su centroide (orejas interiores rosas).
func shrink(pts []point, factor float64) []point {
	var cx, cy float64
	for _, p := range pts {
		cx += p.x
		cy += p.y
	}
	cx /= float64(len(pts))
	cy /= float64(len(pts))

	out := make([]point, len(pts))
	for i, p := range pts {
		out[i] = point{cx + (p.x-cx)*factor, cy + (p.y-cy)*factor}
	}
	return out
}

// -----------------------------------------------------------------------------
// Icono del System Tray
// -----------------------------------------------------------------------------

// renderIcon dibuja el icono a la resolución pedida. El lienzo de diseño es de
// 100×100 unidades y sólo contiene la cara: a 16×16 píxeles (el tamaño real en
// la bandeja de Windows) cualquier detalle adicional se convierte en ruido.
func renderIcon(size int, p catPalette) *image.NRGBA {
	const supersample = 4
	c := newCanvas(100, 100, float64(size)*supersample/100)

	// Pechera blanca asomando por debajo de la cabeza: completa el esmoquin.
	c.polygon([]point{{38, 84}, {62, 84}, {70, 100}, {30, 100}}, p.white)
	c.ellipse(50, 96, 26, 14, p.fur)
	c.ellipse(50, 100, 15, 11, p.white)

	drawCatFace(c, 50, 52, 36, p)

	// Punto de estado. Va con un halo del color del fondo para que se despegue
	// del pelaje incluso cuando el icono se dibuja sobre una barra de tareas
	// oscura, y se recorta contra el borde del lienzo para ganar tamaño: a
	// 16 px son 4 píxeles de color, que es justo lo que se ve de un vistazo.
	if p.badge != nil {
		c.circle(80, 82, 21, furWhite)
		c.circle(80, 82, 16, *p.badge)
	}

	return c.downsample(supersample)
}

// -----------------------------------------------------------------------------
// Ilustración de bienvenida: el gato jugando con la ticketera
// -----------------------------------------------------------------------------

const (
	welcomeW = 440.0
	welcomeH = 220.0
)

func renderWelcome() *image.NRGBA {
	// scale 4 con supersample 2 => imagen final a 880×440, el doble del tamaño
	// lógico de la ventana, para que se vea nítida en pantallas HiDPI.
	const supersample = 2
	c := newCanvas(welcomeW, welcomeH, 2*supersample)

	c.roundRect(0, 0, welcomeW, welcomeH, 0, panelBG)

	// Sombras de apoyo.
	c.ellipse(130, 188, 100, 11, shadow.alpha(0.07))
	c.ellipse(334, 192, 74, 10, shadow.alpha(0.07))

	drawPrinter(c)
	drawReceipt(c)
	drawCat(c)

	return c.downsample(supersample)
}

func drawPrinter(c *canvas) {
	c.roundRect(52, 104, 202, 186, 13, caseBody) // cuerpo
	c.roundRect(62, 140, 192, 176, 9, caseFront) // panel frontal
	c.roundRect(62, 86, 192, 116, 10, caseFront) // tapa superior
	c.roundRect(74, 93, 180, 103, 5, caseSlot)   // ranura del papel
	c.circle(176, 128, 5, ledGreen)              // led de estado
	c.circle(160, 128, 5, inkGray.alpha(0.75))   // botón de avance
	c.roundRect(74, 152, 150, 160, 4, caseSlot)  // rejilla decorativa
	c.roundRect(74, 164, 122, 170, 3, caseSlot)  //
	c.roundRect(58, 184, 88, 194, 4, caseBody)   // patas
	c.roundRect(166, 184, 196, 194, 4, caseBody) //
}

// receiptCurve es la línea central del ticket que sale de la ranura.
var (
	receiptP0 = point{126, 96}
	receiptP1 = point{140, 26}
	receiptP2 = point{214, 34}
)

// drawReceipt dibuja el ticket saliendo de la ranura: una tira curvada con
// borde, líneas de texto y el dentado del corte en el extremo.
func drawReceipt(c *canvas) {
	strip := func(halfWidth float64, col rgba) {
		const steps = 48
		var left, right []point
		for i := 0; i <= steps; i++ {
			t := float64(i) / steps
			p := quadPoint(receiptP0, receiptP1, receiptP2, t)
			tan := quadTangent(receiptP0, receiptP1, receiptP2, t)
			n := point{-tan.y, tan.x}
			left = append(left, point{p.x + n.x*halfWidth, p.y + n.y*halfWidth})
			right = append(right, point{p.x - n.x*halfWidth, p.y - n.y*halfWidth})
		}
		pts := left
		for i := len(right) - 1; i >= 0; i-- {
			pts = append(pts, right[i])
		}
		c.polygon(pts, col)
	}

	strip(25, paperEdge)
	strip(23.2, paperWhite)

	// Líneas de texto impresas, perpendiculares a la tira.
	type bar struct {
		t     float64
		half  float64
		width float64
		col   rgba
	}
	for _, b := range []bar{
		{0.22, 15, 4.5, inkDark},
		{0.38, 17, 3.0, inkGray},
		{0.52, 17, 3.0, inkGray},
		{0.66, 17, 3.0, inkGray},
		{0.80, 12, 4.5, inkDark},
	} {
		p := quadPoint(receiptP0, receiptP1, receiptP2, b.t)
		tan := quadTangent(receiptP0, receiptP1, receiptP2, b.t)
		n := point{-tan.y, tan.x}
		c.line(
			point{p.x + n.x*b.half, p.y + n.y*b.half},
			point{p.x - n.x*b.half, p.y - n.y*b.half},
			b.width, b.col,
		)
	}

	// Dentado del corte: triángulos del color del fondo mordiendo el extremo.
	end := quadPoint(receiptP0, receiptP1, receiptP2, 1)
	tan := quadTangent(receiptP0, receiptP1, receiptP2, 1)
	n := point{-tan.y, tan.x}
	const teeth = 6
	for i := 0; i < teeth; i++ {
		o0 := 25 - float64(i)*(50/teeth)
		o1 := o0 - (50 / teeth)
		mid := (o0 + o1) / 2
		c.triangle(
			point{end.x + n.x*o0, end.y + n.y*o0},
			point{end.x + n.x*o1, end.y + n.y*o1},
			point{end.x + n.x*mid - tan.x*7, end.y + n.y*mid - tan.y*7},
			panelBG,
		)
	}
}

func drawCat(c *canvas) {
	// Cola, por detrás del cuerpo.
	c.quadStroke(point{382, 172}, point{436, 132}, point{404, 78}, 15, 8, furBlack)
	c.circle(404, 78, 4, furWhite) // punta blanca

	// Cuerpo sentado y pechera.
	c.ellipse(338, 148, 46, 44, furBlack)
	c.ellipse(330, 158, 23, 30, furWhite)

	// Patas traseras apoyadas.
	c.roundRect(310, 166, 332, 192, 11, furBlack)
	c.roundRect(344, 166, 366, 192, 11, furBlack)
	c.ellipse(321, 187, 11, 7, furWhite)
	c.ellipse(355, 187, 11, 7, furWhite)

	// Pata levantada tocando el ticket: es lo que convierte la escena en "jugar
	// con la impresora" y no en "un gato al lado de una impresora".
	c.line(point{308, 138}, point{246, 78}, 19, furBlack)
	c.circle(242, 74, 12.5, furWhite)
	for _, dx := range []float64{-6, 0, 6} {
		c.line(point{242 + dx*0.9, 66}, point{242 + dx, 62}, 3, furWhite)
	}

	drawCatFace(c, 336, 92, 40, basePalette)
}

// -----------------------------------------------------------------------------
// Escritura de archivos
// -----------------------------------------------------------------------------

// icoSize describe una resolución dentro del .ico y cómo se codifica.
type icoSize struct {
	size int
	// asPNG indica que la entrada se guarda comprimida en PNG en lugar de como
	// DIB. Windows lo admite desde Vista y evita que un icono de 256×256
	// engorde el archivo en 260 KB.
	asPNG bool
}

var icoSizes = []icoSize{
	{16, false}, {24, false}, {32, false}, {48, false}, {64, false},
	{128, true}, {256, true},
}

// encodeICO construye el contenedor .ico: cabecera ICONDIR, una ICONDIRENTRY
// por resolución y, a continuación, los datos de cada imagen.
func encodeICO(images map[int]*image.NRGBA) ([]byte, error) {
	var payloads [][]byte

	for _, s := range icoSizes {
		img, ok := images[s.size]
		if !ok {
			return nil, fmt.Errorf("falta la imagen de %dpx", s.size)
		}
		if s.asPNG {
			var buf bytes.Buffer
			if err := png.Encode(&buf, img); err != nil {
				return nil, err
			}
			payloads = append(payloads, buf.Bytes())
			continue
		}
		payloads = append(payloads, encodeICODIB(img))
	}

	var out bytes.Buffer
	// ICONDIR: reservado, tipo 1 (icono), número de imágenes.
	binary.Write(&out, binary.LittleEndian, uint16(0))
	binary.Write(&out, binary.LittleEndian, uint16(1))
	binary.Write(&out, binary.LittleEndian, uint16(len(icoSizes)))

	offset := 6 + 16*len(icoSizes)
	for i, s := range icoSizes {
		dim := byte(s.size) // 256 se codifica como 0
		if s.size >= 256 {
			dim = 0
		}
		out.WriteByte(dim)                                  // ancho
		out.WriteByte(dim)                                  // alto
		out.WriteByte(0)                                    // colores de la paleta
		out.WriteByte(0)                                    // reservado
		binary.Write(&out, binary.LittleEndian, uint16(1))  // planos
		binary.Write(&out, binary.LittleEndian, uint16(32)) // bits por píxel
		binary.Write(&out, binary.LittleEndian, uint32(len(payloads[i])))
		binary.Write(&out, binary.LittleEndian, uint32(offset))
		offset += len(payloads[i])
	}

	for _, p := range payloads {
		out.Write(p)
	}
	return out.Bytes(), nil
}

// encodeICODIB serializa una imagen como DIB de 32 bits dentro de un .ico:
// BITMAPINFOHEADER (con biHeight al doble, por la máscara), píxeles BGRA de
// abajo arriba y una máscara AND vacía —obligatoria aunque el alfa la haga
// redundante—.
func encodeICODIB(img *image.NRGBA) []byte {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	maskRow := ((w + 31) / 32) * 4
	maskSize := maskRow * h

	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, uint32(40))             // biSize
	binary.Write(&out, binary.LittleEndian, int32(w))               // biWidth
	binary.Write(&out, binary.LittleEndian, int32(2*h))             // biHeight (XOR + AND)
	binary.Write(&out, binary.LittleEndian, uint16(1))              // biPlanes
	binary.Write(&out, binary.LittleEndian, uint16(32))             // biBitCount
	binary.Write(&out, binary.LittleEndian, uint32(0))              // biCompression = BI_RGB
	binary.Write(&out, binary.LittleEndian, uint32(w*h*4+maskSize)) // biSizeImage
	binary.Write(&out, binary.LittleEndian, int32(0))               // biXPelsPerMeter
	binary.Write(&out, binary.LittleEndian, int32(0))               // biYPelsPerMeter
	binary.Write(&out, binary.LittleEndian, uint32(0))              // biClrUsed
	binary.Write(&out, binary.LittleEndian, uint32(0))              // biClrImportant

	for y := h - 1; y >= 0; y-- {
		for x := 0; x < w; x++ {
			p := img.NRGBAAt(x, y)
			out.Write([]byte{p.B, p.G, p.R, p.A})
		}
	}
	out.Write(make([]byte, maskSize))

	return out.Bytes()
}

func writePNG(path string, img *image.NRGBA) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// iconSet describe cada uno de los iconos que se escriben en el repositorio.
var iconSets = []struct {
	name    string
	palette catPalette
	purpose string
}{
	{"app_icon", basePalette, "ejecutable, instalador y ventana de bienvenida"},
	{"app_icon_gray", grayPalette, "System Tray — iniciando (aún no escucha)"},
	{"app_icon_green", greenPalette, "System Tray — operativo (servidor escuchando)"},
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}

	for _, set := range iconSets {
		icons := make(map[int]*image.NRGBA, len(icoSizes))
		for _, s := range icoSizes {
			icons[s.size] = renderIcon(s.size, set.palette)
		}

		ico, err := encodeICO(icons)
		if err != nil {
			fatal(err)
		}
		icoPath := filepath.Join(root, set.name+".ico")
		if err := os.WriteFile(icoPath, ico, 0o644); err != nil {
			fatal(err)
		}
		fmt.Printf("%s (%d bytes, %d resoluciones) — %s\n", icoPath, len(ico), len(icoSizes), set.purpose)

		// PNG para la bandeja de macOS: NSImage no acepta .ico de forma fiable.
		pngPath := filepath.Join(root, set.name+".png")
		if err := writePNG(pngPath, icons[64]); err != nil {
			fatal(err)
		}
		fmt.Printf("%s (64x64)\n", pngPath)
	}

	welcomePath := filepath.Join(root, "welcome_cat.png")
	welcome := renderWelcome()
	if err := writePNG(welcomePath, welcome); err != nil {
		fatal(err)
	}
	fmt.Printf("%s (%dx%d)\n", welcomePath, welcome.Bounds().Dx(), welcome.Bounds().Dy())
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "genassets: %v\n", err)
	os.Exit(1)
}
