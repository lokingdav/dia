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

func Evaluate(privateKey, blindedElement []byte) (evaluatedElement []byte, err error) {
	evaluatedElement, err = dia.VOPRFEvaluate(blindedElement, privateKey)
	return
}

func BulkEvaluate(privateKey []byte, elements [][]byte) ([][]byte, error) {
	var err error
	for i, v := range elements {
		elements[i], err = Evaluate(privateKey, v)
	}
	return elements, err
}

func Finalize(evaluatedElement, blind []byte) (result []byte, err error) {
	result, err = dia.VOPRFUnblind(evaluatedElement, blind)
	return result, err
}

func VerifyTicket(ticket, verifyKey []byte) (bool, error) {
	t1, t2 := ticket[0:32], ticket[32:]
	return dia.VOPRFVerify(t1, t2, verifyKey)
}