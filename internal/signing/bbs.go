package signing

import (
	"github.com/lokingdav/libdia/bindings/go"
)

func BbsKeygen() ([]byte, []byte, error) {
	sk, pk, err := dia.BBSKeygen()
	return sk, pk, err
}