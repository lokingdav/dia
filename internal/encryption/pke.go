package encryption

func PkeEncrypt(publicKey, plaintext []byte) ([]byte, error) {
	// todo encrypt
	ctx := plaintext

	return ctx, nil
}

func PkeDecrypt(privateKey, ciphertext []byte) ([]byte, error) {
	// TODO: plaintext := decrypt(ctx, secretKey)
	plaintext := ciphertext

	return plaintext, nil
}
