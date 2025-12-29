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

	// Output server configuration in DIA format
	envString, err := serverCfg.ToEnv()
	if err != nil {
		log.Fatalf("Error serializing config: %v", err)
	}

	fmt.Println("# DIA Server Configuration")
	fmt.Println("# Save this securely - it contains private keys for enrollment processing")
	fmt.Println("# In production, store in HSM, encrypted file, or secure key management service")
	fmt.Println()
	fmt.Println(envString)
}

func main() {
	duration := flag.Int("duration", 30, "Enrollment duration in days")
	flag.Parse()

	registrationAuthoritySetup(*duration)
}
