package bbs

import (
	"github.com/dense-identity/denseid/internal/helpers"
	dia "github.com/lokingdav/libdia/bindings/go"
)

type AkeZkProof struct {
	Tn                                                          string
	PublicKey, Expiration, Nonce, RaPublicKey, Signature, Proof []byte
}

func ZkCreateProof(params AkeZkProof) ([]byte, error) {
	message := helpers.ConcatBytes(params.PublicKey, params.Expiration, []byte(params.Tn))

	return dia.BBSCreateProof(
		[][]byte{message},
		[]uint32{1},
		params.RaPublicKey,
		params.Signature,
		params.Nonce,
	)
}

func ZkVerifyProof(params AkeZkProof) (bool, error) {
	message := helpers.ConcatBytes(params.PublicKey, params.Expiration, []byte(params.Tn))
	return dia.BBSVerifyProof(
		[]uint32{1},
		[][]byte{message},
		params.RaPublicKey,
		params.Nonce,
		params.Proof,
	)
}
