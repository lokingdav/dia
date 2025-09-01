package encryption

func SymEncrypt(secretKey, plaintext []byte) ([]byte, error) {
	// todo encrypt
	ctx := plaintext

	return ctx, nil
}

func SymDecrypt(secretKey, ciphertext []byte) ([]byte, error) {
	// TODO: plaintext := decrypt(ctx, secretKey)
	plaintext := ciphertext

	return plaintext, nil
}
