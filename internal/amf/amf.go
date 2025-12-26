package amf

import (
	"github.com/lokingdav/libdia/bindings/go"
)


func Keygen() ([]byte, []byte, error) {
	sk, pk, err := dia.AMFKeygen()
	return sk, pk, err
}

func Sign(senderSk, receiverPk, judgePk, message []byte) ([]byte, error) {
	return dia.AMFFrank(
		senderSk, 
		receiverPk, 
		judgePk, 
		message,
	)
}

func Verify(senderPk, receiverSk, judgePk, message, signature []byte) (bool, error) {
	return dia.AMFVerify(
		senderPk,
		receiverSk,
		judgePk,
		message,
		signature,
	)
}
