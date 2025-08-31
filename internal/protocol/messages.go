package protocol

type AkeMessage1 struct {
	DhPk, Ciphertext, ZkProof []byte
}

type AkeMessage2 struct {
	AkeMessage1
}

func (m1 *AkeMessage1) VerifyZk() (bool) {
	return false
}

func (m1 *AkeMessage1) Encrypt(secretKey []byte) ([]byte, error) {
	return nil, nil
}

func (m1 *AkeMessage1) Decrypt(secretKey []byte) (error) {
	return nil
}


func (m2 *AkeMessage2) Encrypt(secretKey []byte) (error) {
	return nil
}

func (m2 *AkeMessage2) Decrypt(privateKey []byte) (error) {
	return nil
}