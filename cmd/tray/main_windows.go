//go:build windows

package main

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

func main() {
	portFlag := flag.Int("port", 0, "HTTP server port (0 for random)")
	openBrowser := flag.Bool("open", true, "Open portal UI in browser automatically on launch")
	flag.Parse()

	server, err := StartTrayServer(*portFlag)
	if err != nil {
		log.Fatalf("[FATAL]: failed to start tray portal server: %v", err)
	}
	defer server.Close()

	portalURL := server.GetURL()
	log.Printf("[INFO]: OpenUEM Self-Service Portal started at %s", portalURL)

	if *openBrowser {
		// Open default browser on Windows
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", portalURL).Start()
	}

	// Keep alive until interrupted
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("[INFO]: OpenUEM Self-Service Portal tray application exiting")
}
