package main

import (
	"flag"
	"fmt"
	"log"

	dia "github.com/lokingdav/libdia/bindings/go"
)

func registrationAuthoritySetup(durationDays int) {
	// Generate complete DIA server configuration
	serverCfg, err := dia.GenerateServerConfig(durationDays)
	if err != nil {
		log.Fatalf("Server config generation failed: %v", err)
	}
	defer serverCfg.Close()

	fmt.Println("# DIA Server Configuration Generated")
	fmt.Println("# Duration:", durationDays, "days")
	fmt.Println()
	fmt.Println("# IMPORTANT: Store the server configuration securely!")
	fmt.Println("# The server needs to persist this config to process enrollments.")
	fmt.Println("# You can serialize it using serverCfg methods or save the individual keys.")
	fmt.Println()
	fmt.Println("# For now, this tool demonstrates successful generation.")
	fmt.Println("# In production, implement proper key storage (HSM, encrypted file, etc.)")
	fmt.Println()
	fmt.Println("✓ Server configuration generated successfully")
}

func main() {
	duration := flag.Int("duration", 30, "Enrollment duration in days")
	flag.Parse()

	registrationAuthoritySetup(*duration)
}
