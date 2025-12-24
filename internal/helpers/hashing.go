package helpers

import (
	"crypto/sha256"
	"encoding/hex"
)

func Hash256(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

func HashAll(items ...[]byte) []byte {
	concats := ConcatBytes(items...)
	return Hash256(concats)
}

func HashAllHex(items ...[]byte) string {
	concats := ConcatBytes(items...)
	return hex.EncodeToString(Hash256(concats))
}

func Hash256Hex(data []byte) string {
	return hex.EncodeToString(Hash256(data))
}
