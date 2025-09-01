package voprf

import (
	"github.com/lokingdav/libdia/bindings/go"
)

type BlindedTicket struct {
	Input   []byte
	Blinded []byte
	Blind   []byte
}

type Ticket struct {
	T1 []byte
	T2 []byte
}

func (t *Ticket) ToBytes() []byte {
	return append(t.T1, t.T2...)
}

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

func GenerateTickets(count int) []BlindedTicket {
	blindedTickets := make([]BlindedTicket, count)
	for i := 0; i < count; i++ {
		input, _, _ := Keygen()
		blinded, blind, _ := Blind(input)
		blindedTickets[i] = BlindedTicket{
			Input:   input,
			Blinded: blinded,
			Blind:   blind,
		}
	}
	return blindedTickets
}

func FinalizeTickets(blindedTickets []BlindedTicket, evaluated [][]byte) []Ticket {
	tickets := make([]Ticket, len(blindedTickets))
	for i, v := range evaluated {
		output, _ := Finalize(v, blindedTickets[i].Blind)
		tickets[i] = Ticket{
			T1: blindedTickets[i].Input,
			T2: output,
		}
	}
	return tickets
}

func VerifyTickets(tickets []Ticket, verifyKey []byte) (bool, error) {
	inputs := make([][]byte, len(tickets))
	outputs := make([][]byte, len(tickets))
	for i, v := range tickets {
		inputs[i] = v.T1
		outputs[i] = v.T2
	}
	return dia.VOPRFVerifyBatch(inputs, outputs, verifyKey)
}
