// healthcheck is the bounded container probe used by Lidza Deploy's stack contract.
package main

import (
	"net/http"
	"os"
	"time"
)

func main() {
	address := "http://127.0.0.1:3000/api/v1/health"
	if len(os.Args) == 2 {
		address = os.Args[1]
	}
	client := http.Client{Timeout: 5 * time.Second}
	res, err := client.Get(address)
	if err != nil {
		os.Exit(1)
	}
	if err = res.Body.Close(); err != nil || res.StatusCode != 200 {
		os.Exit(1)
	}
}
