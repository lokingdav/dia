// Package merkle provides a wrapper around the cbergoon/merkletree library
// to handle slices of strings by building a single Merkle tree.
package merkle

import (
	"bytes"
	"crypto/sha256"
	"errors"

	"github.com/cbergoon/merkletree"
)

// stringContent implements the merkletree.Content interface for a simple string.
type stringContent struct {
	value string
}

// CalculateHash hashes the data of a stringContent object.
func (s stringContent) CalculateHash() ([]byte, error) {
	h := sha256.New()
	if _, err := h.Write([]byte(s.value)); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// Equals tests for equality of two stringContent objects.
func (s stringContent) Equals(other merkletree.Content) (bool, error) {
	// Type assert the other content to stringContent
	otherContent, ok := other.(stringContent)
	if !ok {
		return false, errors.New("value is not of type stringContent")
	}
	return s.value == otherContent.value, nil
}

// Proof is a simple struct to hold the components of a Merkle inclusion proof.
type Proof struct {
	// Path contains the sibling hashes from the leaf to the root.
	Path [][]byte
	// Index indicates the position of each sibling hash (0 for left, 1 for right).
	Index []int64
}

// flattenAndConvert takes a 2D string slice, flattens it into a 1D slice,
// and converts each string into a merkletree.Content object.
func flattenAndConvert(data [][]string) []merkletree.Content {
	var contentList []merkletree.Content
	for _, innerSlice := range data {
		for _, item := range innerSlice {
			contentList = append(contentList, stringContent{value: item})
		}
	}
	return contentList
}

// CreateRoot calculates the Merkle root for a 2D slice of strings.
// It flattens the data into a single list before building the tree.
func CreateRoot(data [][]string) ([]byte, error) {
	contentList := flattenAndConvert(data)
	if len(contentList) == 0 {
		return nil, errors.New("cannot create root from empty data")
	}

	tree, err := merkletree.NewTree(contentList)
	if err != nil {
		return nil, err
	}

	return tree.MerkleRoot(), nil
}

// GenerateProof generates an inclusion proof for a single item within the 2D slice.
// Note: To get proofs for multiple items, call this function for each item.
func GenerateProof(data [][]string, item string) (*Proof, error) {
	contentList := flattenAndConvert(data)
	if len(contentList) == 0 {
		return nil, errors.New("cannot generate proof from empty data")
	}

	tree, err := merkletree.NewTree(contentList)
	if err != nil {
		return nil, err
	}

	// Find the content object for the item we want to prove
	itemContent := stringContent{value: item}

	// Rely on the library to find the path. If it can't, it will return an error.
	path, indexes, err := tree.GetMerklePath(itemContent)
	if err != nil {
		return nil, err
	}

	return &Proof{Path: path, Index: indexes}, nil
}

// VerifyProof verifies an inclusion proof for a single item against a known root hash.
// This function implements the verification logic manually by reconstructing the
// root from the leaf hash and the proof path.
func VerifyProof(rootHash []byte, proof *Proof, item string) (bool, error) {
	itemContent := stringContent{value: item}
	itemHash, err := itemContent.CalculateHash()
	if err != nil {
		return false, err
	}

	computedHash := itemHash
	h := sha256.New()

	for i := 0; i < len(proof.Path); i++ {
		h.Reset()
		siblingHash := proof.Path[i]
		siblingIndex := proof.Index[i]

		if siblingIndex == 0 { // Sibling is on the left, so current hash is on the right.
			h.Write(siblingHash)
			h.Write(computedHash)
		} else { // Sibling is on the right, so current hash is on the left.
			h.Write(computedHash)
			h.Write(siblingHash)
		}
		computedHash = h.Sum(nil)
	}

	return bytes.Equal(computedHash, rootHash), nil
}
