package main

type PrinterInfo struct {
	Name string `json:"name"`
}

type PrintRequest struct {
	PrinterName string `json:"printer_name"`
	PrinterData string `json:"printer_data"`
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
