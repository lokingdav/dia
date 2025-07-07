package main

import (
	"fmt"
	"log"

	// Replace 'your-module' with your actual Go module path if it's different.
	// This assumes the merkle package is located at 'your-module/internal/merkle'.
	"github.com/dense-identity/denseid/internal/merkle"
)

func main() {
	// 1. Define your 2D data.
	data := [][]string{
		{"apple", "banana", "cherry"},
		{"date", "elderberry"},
		{"fig", "grape", "honeydew", "kiwi"},
	}

	// 2. Create the Merkle root for the entire dataset.
	root, err := merkle.CreateRoot(data)
	if err != nil {
		log.Fatalf("Failed to create root: %v", err)
	}
	fmt.Printf("Merkle Root: %x\n\n", root)

	// --- Test Case 1: A valid item ---
	itemToProve := "elderberry"
	fmt.Printf("--- Verifying a valid item: '%s' ---\n", itemToProve)

	proof, err := merkle.GenerateProof(data, itemToProve)
	if err != nil {
		log.Fatalf("Failed to generate proof for '%s': %v", itemToProve, err)
	}
	fmt.Printf("Proof generated successfully.\n")

	isValid, err := merkle.VerifyProof(root, proof, itemToProve)
	if err != nil {
		log.Fatalf("Error during verification: %v", err)
	}
	fmt.Printf("Verification result: %t\n\n", isValid) // Should be true

	// --- Test Case 2: An invalid item ---
	invalidItem := "mango"
	fmt.Printf("--- Attempting to verify a non-existent item: '%s' ---\n", invalidItem)

	// Attempt to generate a proof for an item that doesn't exist.
	invalidProof, err := merkle.GenerateProof(data, invalidItem)
	if err != nil {
		// This is the ideal and expected outcome.
		fmt.Printf("Correctly failed to generate proof: %v\n\n", err)
	} else {
		// If a proof was somehow generated, we MUST ensure it fails verification.
		log.Println("Warning: Proof was generated for a non-existent item. Verifying it must fail.")
		isValid, _ := merkle.VerifyProof(root, invalidProof, invalidItem)
		fmt.Printf("Verification result: %t\n\n", isValid)
		if isValid {
			log.Fatalf("CRITICAL SECURITY FLAW: A proof for a non-existent item was successfully verified.")
		}
	}

	// --- Test Case 3: A valid proof with the wrong item ---
	fmt.Printf("--- Verifying a valid proof with the wrong item: '%s' ---\n", "apple")

	// Use the valid proof from 'elderberry' to try to verify 'apple'.
	isTamperedValid, err := merkle.VerifyProof(root, proof, "apple")
	if err != nil {
		log.Fatalf("Error during verification: %v", err)
	}
	// This should be false because the proof for 'elderberry' is not valid for 'apple'.
	fmt.Printf("Verification result: %t\n", isTamperedValid)
}
