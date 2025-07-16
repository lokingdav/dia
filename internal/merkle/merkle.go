package merkle

import (
    "bytes"
    "crypto/sha256"
    "crypto/subtle"
    "encoding/base64"
    "encoding/gob"
    "errors"
)

// domain separation markers
const (
    leafPrefix byte = 0x00
    nodePrefix byte = 0x01
)

// CreateRoot computes the Merkle root of the given non-empty slice of UTF-8 strings.
func CreateRoot(items []string) ([]byte, error) {
    if len(items) == 0 {
        return nil, errors.New("item list cannot be empty")
    }
    leaves := make([][]byte, len(items))
    for i, s := range items {
        leaves[i] = leafHash([]byte(s))
    }
    return buildTree(leaves), nil
}

// GenerateProof builds an inclusion proof for `item` within `items`.
// Returns (proof, nil) if found, or (nil, nil) if item ∉ items.
func GenerateProof(items []string, item string) (*MerkleProof, error) {
    var index = -1
    for i, s := range items {
        if s == item {
            index = i
            break
        }
    }
    if index < 0 {
        return nil, nil
    }

    level := make([][]byte, len(items))
    for i, s := range items {
        level[i] = leafHash([]byte(s))
    }

    hashes := make([][]byte, 0, 32)
    dirs   := make([]bool, 0, 32)
    idx := index

    for len(level) > 1 {
        pairIndex := idx ^ 1 // flip last bit: even→odd, odd→even
        var sibling []byte
        if pairIndex < len(level) {
            sibling = level[pairIndex]
        } else {
            sibling = level[idx] // duplicate when odd-length
        }
        isLeft := pairIndex < idx
        hashes = append(hashes, sibling)
        dirs   = append(dirs, isLeft)

        level = nextLevel(level)
        idx   = idx / 2
    }

    return &MerkleProof{
        Hashes:     hashes,
        Directions: dirs,
    }, nil
}

// VerifyProof checks that `proof` shows membership of `item` under `root`.
func VerifyProof(root []byte, item string, proof *MerkleProof) bool {
    // start from the leaf hash
    computed := leafHash([]byte(item))
    for i, sibling := range proof.Hashes {
        if proof.Directions[i] {
            computed = nodeHash(sibling, computed)
        } else {
            computed = nodeHash(computed, sibling)
        }
    }
    return subtle.ConstantTimeCompare(root, computed) == 1
}

func leafHash(data []byte) []byte {
    h := sha256.New()
    h.Write([]byte{leafPrefix})
    h.Write(data)
    return h.Sum(nil)
}

func nodeHash(left, right []byte) []byte {
    h := sha256.New()
    h.Write([]byte{nodePrefix})
    h.Write(left)
    h.Write(right)
    return h.Sum(nil)
}

func buildTree(level [][]byte) []byte {
    if len(level) == 1 {
        return level[0]
    }
    return buildTree(nextLevel(level))
}

func nextLevel(nodes [][]byte) [][]byte {
    n := (len(nodes) + 1) / 2
    out := make([][]byte, 0, n)
    for i := 0; i < len(nodes); i += 2 {
        left := nodes[i]
        var right []byte
        if i+1 < len(nodes) {
            right = nodes[i+1]
        } else {
            right = left
        }
        out = append(out, nodeHash(left, right))
    }
    return out
}

// MerkleProof is a typed inclusion proof.
type MerkleProof struct {
    Hashes     [][]byte // sibling hashes along the path
    Directions []bool   // true=that sibling is to the left
}

// ToBytes serializes the proof using encoding/gob.
func (p *MerkleProof) ToBytes() ([]byte, error) {
    var buf bytes.Buffer
    enc := gob.NewEncoder(&buf)
    if err := enc.Encode(p); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}

// ToBase64 returns a Base64-encoded gob serialization, suitable for JSON strings.
func (p *MerkleProof) ToBase64() (string, error) {
    b, err := p.ToBytes()
    if err != nil {
        return "", err
    }
    return base64.StdEncoding.EncodeToString(b), nil
}

// ProofFromBytes deserializes a gob-encoded proof.
func ProofFromBytes(data []byte) (*MerkleProof, error) {
    buf := bytes.NewBuffer(data)
    dec := gob.NewDecoder(buf)
    var p MerkleProof
    if err := dec.Decode(&p); err != nil {
        return nil, err
    }
    return &p, nil
}

// ProofFromBase64 decodes Base64 then gob-unmarshals a MerkleProof.
func ProofFromBase64(str string) (*MerkleProof, error) {
    data, err := base64.StdEncoding.DecodeString(str)
    if err != nil {
        return nil, err
    }
    return ProofFromBytes(data)
}
