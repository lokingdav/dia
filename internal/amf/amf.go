package amf

import (
	"github.com/lokingdav/libdia/bindings/go"
)

type AmfKey struct {
	SenderSk, SenderPk []byte
	ReceiverSk, ReceiverPk []byte
	JudgeSk, JudgePk []byte
}

func Keygen() ([]byte, []byte, error) {
	sk, pk, err := dia.AMFKeygen()
	return sk, pk, err
}

func Sign(keys *AmfKey, message []byte) ([]byte, error) {
	return dia.AMFFrank(
		keys.SenderSk, 
		keys.ReceiverPk, 
		keys.JudgePk, 
		message,
	)
}

func Verify(keys AmfKey, message, signature []byte) (bool, error) {
	return dia.AMFVerify(
		keys.SenderPk,
		keys.ReceiverSk,
		keys.JudgePk,
		message,
		signature,
	)
}
