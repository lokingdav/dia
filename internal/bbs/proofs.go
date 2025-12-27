package bbs

import (
	"github.com/dense-identity/denseid/internal/helpers"
	dia "github.com/lokingdav/libdia/bindings/go"
)

type AkeZkProof struct {
	Tn, Name                                                                     string
	AmfPublicKey, PkePublicKey, Expiration, Nonce, RaPublicKey, Signature, Proof []byte
}

func ZkCreateProof(params AkeZkProof) ([]byte, error) {
	message1 := helpers.HashAll(params.AmfPublicKey, params.PkePublicKey, params.Expiration, []byte(params.Tn))
	message2 := []byte(params.Name)

	return dia.BBSCreateProof(
		[][]byte{message1, message2},
		[]uint32{1},
		params.RaPublicKey,
		params.Signature,
		params.Nonce,
	)
}

func ZkVerifyProof(params AkeZkProof) (bool, error) {
	message1 := helpers.HashAll(params.AmfPublicKey, params.PkePublicKey, params.Expiration, []byte(params.Tn))
	return dia.BBSVerifyProof(
		[]uint32{1},
		[][]byte{message1},
		params.RaPublicKey,
		params.Nonce,
		params.Proof,
	)
}
