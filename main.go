package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"
)

func main() {
	port, err := resolvePort(os.Args[1:], os.Getenv("PORT"))
	if err != nil {
		log.Fatal(err)
	}
	app, err := newServer()
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()
	host := os.Getenv("HOST")
	if host == "" {
		host = "0.0.0.0"
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	server := &http.Server{
		Addr:              address,
		Handler:           app,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	fmt.Printf("orby listening on http://%s\n", address)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
