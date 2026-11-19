//go:build !windows

package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	portFlag := flag.Int("port", 0, "HTTP server port (0 for random)")
	flag.Parse()

	server, err := StartTrayServer(*portFlag)
	if err != nil {
		log.Fatalf("[FATAL]: failed to start tray portal server: %v", err)
	}
	defer server.Close()

	log.Printf("[INFO]: OpenUEM Self-Service Portal started at %s (non-Windows preview)", server.GetURL())

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("[INFO]: OpenUEM Self-Service Portal exiting")
}
