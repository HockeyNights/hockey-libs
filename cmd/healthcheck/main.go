package main

import (
	"net/http"
	"os"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		os.Exit(2)
	}

	client := &http.Client{Timeout: 3 * time.Second}

	// Адрес берётся из аргумента командной строки: healthcheck для этого и
	// существует, его вызывает docker compose со своим же URL. Внешнего ввода
	// здесь нет, поэтому SSRF-предупреждение неприменимо.
	response, err := client.Get(os.Args[1]) // #nosec G704
	if err != nil {
		os.Exit(1)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}
