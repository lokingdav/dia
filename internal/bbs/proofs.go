package bbs

// import "errors"

func ZkProof(chal []byte) ([]byte, error) {
	return []byte("proof"), nil
}

func ZkVerify(proof, chal []byte, tn string) bool {
	return true
}
