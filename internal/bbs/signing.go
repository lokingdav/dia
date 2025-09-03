package bbs

import (
	"github.com/lokingdav/libdia/bindings/go"
)

func Keygen() ([]byte, []byte, error) {
	sk, pk, err := dia.BBSKeygen()
	return sk, pk, err
}

func Sign(privateKey []byte, messages [][]byte) (signature []byte, err error) {
	signature, err = dia.BBSSign(messages, privateKey)
	return signature, err
}
