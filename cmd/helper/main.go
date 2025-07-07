package main

import (
	"log"
	"flag"
	"github.com/dense-identity/denseid/internal/signing"
)

var (
	isKeygen *bool = flag.Bool("keygen", false, "Generate ed25519 Keypair")
)

func main() {
	flag.Parse()

	if *isKeygen {
		log.Print("Generating public-private keypairs...")
		publicKey, privateKey, err := signing.KeyGen()

		if err != nil {
			log.Fatalf("Failed to Generate ed25519 Keypairs: %v", err)
		}

		log.Printf("Generated Keypairs\n\nPUBLIC_KEY=%x\nPRIVATE_KEY=%x\n\n", publicKey, privateKey)
	}
}