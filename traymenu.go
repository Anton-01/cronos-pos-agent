package main

import (
	"fmt"
	"log"
	"sync"

	"github.com/getlantern/systray"
)

// "Imprimir Ticket de Prueba" submenu.
//
// Why the slots are pre-allocated. getlantern/systray can add menu items but
// never remove them, so a submenu rebuilt from a fresh printer list would grow
// by one stale entry per refresh. Instead the submenu is built once with a fixed
// number of slots, all hidden, and refreshing only re-labels and re-shows the
// ones that are needed. Each slot keeps its own goroutine on its ClickedCh for
// the lifetime of the agent, and reads which printer it currently stands for
// from printerSlots — which is what makes the refresh safe while a click is in
// flight.
//
// trayPrinterSlots is the ceiling on printers offered in the menu. A till has
// one or two; a back office might have half a dozen queues including the PDF
// and XPS writers Windows installs by itself. Twelve covers that with room to
// spare, and anything beyond it is reachable through POST /api/print/test.
const trayPrinterSlots = 12

var (
	printerSlotsMu sync.Mutex
	printerSlots   []string
)

// buildTestTicketMenu creates the "Imprimir Ticket de Prueba" item, its slots
// and the goroutines that serve them. It is called once, from onReady.
func buildTestTicketMenu() *systray.MenuItem {
	menu := systray.AddMenuItem("Imprimir Ticket de Prueba", "Imprime un ticket con los datos técnicos de la impresora y el juego de caracteres completo")

	items := make([]*systray.MenuItem, 0, trayPrinterSlots)
	for i := 0; i < trayPrinterSlots; i++ {
		item := menu.AddSubMenuItem("", "")
		item.Hide()
		items = append(items, item)

		go watchPrinterSlot(i, item)
	}

	refresh := menu.AddSubMenuItem("Actualizar lista de impresoras", "Vuelve a leer las impresoras instaladas en el sistema")
	go func() {
		for {
			select {
			case <-refresh.ClickedCh:
				refreshPrinterMenu(items)
			case <-agentDone:
				return
			}
		}
	}()

	refreshPrinterMenu(items)
	return menu
}

// watchPrinterSlot serves one slot for the life of the agent. The printer name
// is looked up on every click instead of being captured when the goroutine
// starts, so that a slot re-labelled by a refresh prints to the printer the
// operator is actually reading on screen.
//
// Printing runs in its own goroutine: rawPrint blocks until the spooler accepts
// the job, and a printer that is off or out of paper can hold that for seconds.
// Blocking here would freeze this slot, and on Windows it would leave the menu
// unresponsive while the operator waits.
func watchPrinterSlot(index int, item *systray.MenuItem) {
	for {
		select {
		case <-item.ClickedCh:
			name := printerSlotName(index)
			if name == "" {
				continue
			}
			go func() {
				log.Printf("[test-ticket] Ticket de prueba solicitado para '%s'", name)
				if err := PrintTestTicket(name, 0); err != nil {
					log.Printf("[test-ticket] Error imprimiendo en '%s': %v", name, err)
					return
				}
				log.Printf("[test-ticket] Ticket de prueba enviado a '%s'", name)
			}()
		case <-agentDone:
			return
		}
	}
}

// printerSlotName returns the printer a slot currently stands for, or "" when
// the slot is hidden.
func printerSlotName(index int) string {
	printerSlotsMu.Lock()
	defer printerSlotsMu.Unlock()

	if index >= len(printerSlots) {
		return ""
	}
	return printerSlots[index]
}

// refreshPrinterMenu re-reads the printers installed on the system and maps them
// onto the slots: the first ones get a name and become visible, the rest are
// hidden.
//
// A system with no printers is not an error and must not leave an empty submenu
// the operator can click into and get nothing from: the first slot is used to
// say so, disabled, which is a clearer answer than a blank menu.
func refreshPrinterMenu(items []*systray.MenuItem) {
	printers, err := discoverPrinters()
	if err != nil {
		log.Printf("[test-ticket] No se pudieron leer las impresoras: %v", err)
		printers = nil
	}

	names := make([]string, 0, len(printers))
	for _, p := range printers {
		names = append(names, p.Name)
	}
	if len(names) > len(items) {
		names = names[:len(items)]
	}

	printerSlotsMu.Lock()
	printerSlots = names
	printerSlotsMu.Unlock()

	if len(names) == 0 {
		items[0].SetTitle("No hay impresoras instaladas")
		items[0].Disable()
		items[0].Show()
		for _, item := range items[1:] {
			item.Hide()
		}
		return
	}

	for i, item := range items {
		if i >= len(names) {
			item.Hide()
			continue
		}
		item.SetTitle(names[i])
		item.SetTooltip(fmt.Sprintf("Imprimir el ticket de prueba en '%s'", names[i]))
		item.Enable()
		item.Show()
	}
}
