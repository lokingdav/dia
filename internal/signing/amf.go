package signing

import (
	"github.com/lokingdav/libdia/bindings/go"
)

func AmfKeygen() ([]byte, []byte, error) {
	sk, pk, err := dia.AMFKeygen()
	return sk, pk, err
}
