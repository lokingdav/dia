package voprf

import (
	"github.com/lokingdav/libdia/bindings/go"
)

func Keygen() ([]byte, []byte, error) {
	sk, pk, err := dia.VOPRFKeygen()
	return sk, pk, err
}

func Blind(input []byte) (blindedElement, blind []byte, err error) {
	blindedElement, blind, err = dia.VOPRFBlind(input)
	if err != nil {
		return nil, nil, err
	}
	return blindedElement, blind, nil
}

func Evaluate(privateKeyBytes, blindedElement []byte) (evaluatedElement []byte, err error) {
	evaluatedElement, err = dia.VOPRFEvaluate(blindedElement, privateKeyBytes)
	return
}

func Finalize(evaluatedElement, blind []byte) (result []byte, err error) {
	result, err = dia.VOPRFUnblind(evaluatedElement, blind)
	return result, err
}
