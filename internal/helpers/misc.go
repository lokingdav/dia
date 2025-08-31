package helpers

import (
	"encoding/hex"
	"runtime"
)

func WipeBytes(b []byte) {
    for i := range b { b[i] = 0 }
    // Ensure the compiler/runtime keeps 'b' alive until after the overwrite.
    runtime.KeepAlive(b)
}

func ConcatBytes(chunks ...[]byte) []byte {
    // Compute total size
    total := 0
    for _, c := range chunks {
        total += len(c)
    }
    // Single allocation
    out := make([]byte, total)

    // Copy in
    i := 0
    for _, c := range chunks {
        i += copy(out[i:], c)
    }
    return out
}

func EncodeToHex(data []byte) (string) {
    return hex.EncodeToString(data)
}
