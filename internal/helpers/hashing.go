package helpers

import (
	"crypto/sha256"
	"encoding/hex"
)

func Hash256(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

func Hash256Hex(data []byte) string {
	return hex.EncodeToString(Hash256(data))
}
