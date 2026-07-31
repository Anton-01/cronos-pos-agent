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

type PDFPrintRequest struct {
	PrinterName string `json:"printer_name"`
	PDFData     string `json:"pdf_data"`
}
