package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/dense-identity/denseid/internal/signing"
	"github.com/dense-identity/denseid/internal/voprf"
)

func registrationAuthoritySetup() []string {
	var env = make([]string, 0, 12)

	// 1. Generate BBS keypair
	bbsSk, bbsPk, err := signing.BbsKeygen()
	if err != nil {
		log.Fatalf("CI Keygen failed: %v", err)
	}
	env = append(env, "# Credential Issuing",
		fmt.Sprintf("CI_SK=%s", signing.EncodeToHex(bbsSk)),
		fmt.Sprintf("CI_PK=%s", signing.EncodeToHex(bbsPk)))

	// 2. Generate OPRF keypair for access throttling
	atSk, atPk, err := voprf.Keygen()
	if err != nil {
		log.Fatalf("Access Throttling Keygen failed: %v", err)
	}
	env = append(env,
		"# Access Throttling",
		fmt.Sprintf("AT_SK=%s", signing.EncodeToHex(atSk)),
		fmt.Sprintf("AT_VK=%s", signing.EncodeToHex(atPk)))

	// 3. Generate OPRF keypair for Key Derivation
	ksSk, ksPk, err := voprf.Keygen()
	if err != nil {
		log.Fatalf("Key Derivation Keygen failed: %v", err)
	}
	env = append(env,
		"# Key Server",
		fmt.Sprintf("KS_SK=%s", signing.EncodeToHex(ksSk)),
		fmt.Sprintf("KS_PK=%s", signing.EncodeToHex(ksPk)))

	// 4. Generate AMF keypair for Moderation
	amfSk, amfPk, err := voprf.Keygen()
	if err != nil {
		log.Fatalf("AMF Keygen failed: %v", err)
	}
	env = append(env, "# Moderation",
		fmt.Sprintf("AMF_SK=%s", signing.EncodeToHex(amfSk)),
		fmt.Sprintf("AMF_PK=%s", signing.EncodeToHex(amfPk)))

	return env
}

func subscriberSetup() []string {
	var env = make([]string, 0, 2)
	sk, pk, err := signing.RegSigKeyGen()
	if err != nil {
		log.Fatalf("Subscriber (ISK, IPK) gen failed: %v", err)
	}
	env = append(env, "# Identity Key with RA",
		fmt.Sprintf("S_ISK=%s", signing.EncodeToHex(sk)),
		fmt.Sprintf("S_IPK=%s", signing.EncodeToHex(pk)))

	return env
}

func main() {
	var (
		party *string = flag.String("type", "", "Allowed values are RA or S")
	)
	flag.Parse()

	var env []string

	switch *party {
	case "RA":
		env = registrationAuthoritySetup()
	case "S":
		env = subscriberSetup()
	}

	if env == nil {
		log.Fatal("Please specify --type either RA or S")
	} else {
		for _, v := range env {
			fmt.Println(v)
		}
	}
}
