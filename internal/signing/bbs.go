package signing

import (
	"github.com/lokingdav/libdia/bindings/go"
)

func BbsKeygen() ([]byte, []byte, error) {
	sk, pk, err := dia.BBSKeygen()
	return sk, pk, err
}

func BbsSign(privateKey []byte, attributes []string) (signature []byte, err error) {
	var messages [][]byte
	for _, message := range attributes {
		messages = append(messages, []byte(message))
	}
	signature, err = dia.BBSSign(messages, privateKey)
	return signature, err
}
