package main

import (
	"fmt"
	"log"
	"net"
)

const (
	defaultPort = 9100
	maxPortScan = 10
)

func ResolvePort(configPort int) (int, error) {
	if configPort > 0 {
		if isPortAvailable(configPort) {
			return configPort, nil
		}
		log.Printf("[network] Puerto configurado %d ocupado, buscando alternativa...", configPort)
	}

	if configPort != defaultPort && isPortAvailable(defaultPort) {
		return defaultPort, nil
	}

	for offset := 1; offset <= maxPortScan; offset++ {
		candidate := defaultPort + offset
		if isPortAvailable(candidate) {
			log.Printf("[network] Puerto alternativo encontrado: %d", candidate)
			return candidate, nil
		}
	}

	return 0, fmt.Errorf("no se encontró puerto libre entre %d y %d", defaultPort, defaultPort+maxPortScan)
}

func isPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}
