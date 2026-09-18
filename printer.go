package main

type PrinterInfo struct {
	Name string `json:"name"`
}

type PrintRequest struct {
	PrinterName string `json:"printer_name"`
	PrinterData string `json:"printer_data"`
	// CodePage permite forzar la página de códigos ESC/POS de este ticket
	// (cp850, cp858, cp1252, cp437 o none). Vacío usa la de config.json.
	CodePage string `json:"code_page,omitempty"`
	// Transcode permite desactivar puntualmente la conversión UTF-8 → página
	// de códigos. Nil usa el valor de config.json.
	Transcode *bool `json:"transcode,omitempty"`
}

// CalibrationRequest pide el ticket de calibración de acentos para una
// impresora. Es el único modo de averiguar qué página de códigos decodifica de
// verdad: ESC/POS no tiene ninguna orden para preguntárselo.
type CalibrationRequest struct {
	PrinterName string `json:"printer_name"`
}

// CalibrationConfirmRequest cierra la calibración: el número de la línea que el
// operador ha visto impresa correctamente en el ticket de prueba.
type CalibrationConfirmRequest struct {
	PrinterName string `json:"printer_name"`
	// Option es el número entre corchetes de esa línea. El valor 0 declara que
	// ninguna era correcta, lo que fija la impresora en modo compatible de
	// forma permanente: imprimirá "Animo" antes que "╡nimo".
	Option int `json:"option"`
	// CodePageID permite además fijar el "n" de "ESC t n" para esta impresora,
	// para las clónicas que numeran sus páginas de otra forma. Nil = estándar.
	CodePageID *int `json:"code_page_id,omitempty"`
}

type PrintJob struct {
	ID           int    `json:"id"`
	DocumentName string `json:"document_name"`
	State        string `json:"state"`
}

type QueueInfo struct {
	PrinterName string     `json:"printer_name"`
	JobsCount   int        `json:"jobs_count"`
	Status      string     `json:"status"`
	Jobs        []PrintJob `json:"jobs,omitempty"`
}

// PDFPrintRequest carries a PDF document to a conventional printer. The
// payload field is "printer_data", the same name PrintRequest uses for a RAW
// ticket: the frontend speaks one vocabulary to both print endpoints, so a
// single agentFetch wrapper serves them and nobody has to remember which
// endpoint renamed the field. It used to be "pdf_data" here, which silently
// turned every PDF job into a 400.
type PDFPrintRequest struct {
	PrinterName string `json:"printer_name"`
	PrinterData string `json:"printer_data"`
}
