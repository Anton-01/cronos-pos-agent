package main

type PrinterInfo struct {
	Name string `json:"name"`
}

type PrintRequest struct {
	PrinterName string `json:"printer_name"`
	PrinterData string `json:"printer_data"`
}
