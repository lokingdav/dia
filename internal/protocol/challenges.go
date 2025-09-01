package protocol

import "github.com/dense-identity/denseid/internal/helpers"

func AkeChallenge0(sharedKey, dhPk []byte, callerId, ts string) []byte {
	data := helpers.ConcatBytes(sharedKey, dhPk, []byte(callerId), []byte(ts))
	return helpers.Hash256(data)
}
