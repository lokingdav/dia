package bbs

import "errors"

func ZkProof(chal []byte) ([]byte, error) {
	return nil, errors.New("not implemented")
}

func ZkVerify(proof, chal []byte, tn string) (bool) {
	return true
}