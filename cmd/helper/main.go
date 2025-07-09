package main

import (
	"encoding/hex"
	"flag"
	"log"
	"strings"

	"github.com/dense-identity/bbsgroupsig/bindings/go"
	"github.com/dense-identity/denseid/internal/signing"
)

var (
	isKeygen *bool   = flag.Bool("keygen", false, "Generate ed25519 Keypair")
	count    *int    = flag.Int("count", 1, "Number of keypairs to generate")
	sigType  *string = flag.String("type", "rs", "Whether rs or gs")
)

func generateRsKeys(count int) {
	publicKeys, privateKeys := []string{}, []string{}

	log.Printf("Generating %d public-private keypairs...\n", count)
	for i := 0; i < count; i++ {
		pk, sk, err := signing.RegSigKeyGen()
		if err == nil {
			publicKeys = append(publicKeys, hex.EncodeToString(pk))
			privateKeys = append(privateKeys, hex.EncodeToString(sk))
		} else {
			log.Fatalf("Failed to Generate ed25519 Keypairs: %v", err)
		}
	}

	log.Printf("Generated Keypairs\n\nPUBLIC_KEY=%s\nPRIVATE_KEY=%s\n\n", strings.Join(publicKeys, ","), strings.Join(privateKeys, ","))
}

func generateGsKeys() {
	bbsgs.InitPairing()
	gpk, osk, isk, err := bbsgs.Setup()
	if err != nil {
		log.Fatalf("Failed to setup group parameters: %v", err)
	}

	log.Printf("Generated Group Parameters:\n\nGROUP_PK=%s\nGROUP_OSK=%s\nGROUP_ISK=%s\n",
		signing.EncodeToString(gpk),
		signing.EncodeToString(osk),
		signing.EncodeToString(isk))
}

func main() {
	flag.Parse()

	if *isKeygen {
		switch *sigType {
		case "rs":
			generateRsKeys(*count)
		case "gs":
			generateGsKeys()
		default:
			log.Fatal("Please specify --type either rs or gs")
		}
	}
}
