package amf

import (
	"github.com/lokingdav/libdia/bindings/go"
)

func Keygen() ([]byte, []byte, error) {
	sk, pk, err := dia.AMFKeygen()
	return sk, pk, err
}