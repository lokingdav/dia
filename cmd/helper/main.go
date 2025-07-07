package main

import (
	"encoding/hex"
	"flag"
	"log"
	"strings"

	"github.com/dense-identity/denseid/internal/signing"
)

var (
	isKeygen *bool = flag.Bool("keygen", false, "Generate ed25519 Keypair")
	count *int = flag.Int("count", 1, "Number of keypairs to generate")
)

func main() {
	flag.Parse()

	if *isKeygen {
		publicKeys, privateKeys := []string{}, []string{}

		log.Printf("Generating %d public-private keypairs...\n", *count)
		for i := 0; i < *count; i++ {
			pk, sk, err := signing.KeyGen()
			if err == nil {
				publicKeys = append(publicKeys, hex.EncodeToString(pk))
				privateKeys = append(privateKeys, hex.EncodeToString(sk))
			} else {
				log.Fatalf("Failed to Generate ed25519 Keypairs: %v", err)
			}
		}

		log.Printf("Generated Keypairs\n\nPUBLIC_KEY=%s\nPRIVATE_KEY=%s\n\n", strings.Join(publicKeys, ","), strings.Join(privateKeys, ","))
	}
}