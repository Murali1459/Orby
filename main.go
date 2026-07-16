package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
)

func main() {
	port, err := resolvePort(os.Args[1:], os.Getenv("PORT"))
	if err != nil {
		log.Fatal(err)
	}
	server, err := newServer()
	if err != nil {
		log.Fatal(err)
	}
	host := os.Getenv("HOST")
	if host == "" {
		host = "0.0.0.0"
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	fmt.Printf("orby listening on http://%s\n", address)
	err = http.ListenAndServe(address, server)
	server.Close()
	log.Fatal(err)
}
