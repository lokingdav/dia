package voprf

import (
	"github.com/dense-identity/bbsgroupsig/bindings/go"
)

func KeyGen() ([]byte, error) {
	return bbsgs.ScalarRandom()
}

func Blind(input []byte) (blind, blindedElement []byte, err error) {
	var point []byte
	blind, err = KeyGen()
	if err != nil {
		return
	}
	point, err = bbsgs.G1HashToPoint(input)
	if err != nil {
		return
	}
	blindedElement, err = bbsgs.G1Mul(point, blind)
	if err != nil {
		return
	}
	return
}

func Evaluate(privateKeyBytes, blindedElement []byte) (evaluatedElement []byte, err error) {
	evaluatedElement, err = bbsgs.G1Mul(blindedElement, privateKeyBytes)
	return
}

func Finalize(blind, evaluatedElement []byte) (result []byte, err error) {
	var blindInv []byte

	blindInv, err = bbsgs.ScalarInverse(blind)
	if err != nil {
		return
	}

	result, err = bbsgs.G1Mul(evaluatedElement, blindInv)
	return
}
