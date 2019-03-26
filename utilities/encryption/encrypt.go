package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
)

//! This internal tool was created with this help:
//! https://tutorialedge.net/golang/go-encrypt-decrypt-aes-tutorial/

// EncryptContent is a function used to encrypt []byte 
// content using AES encryption
func EncryptContent(contentToEncrypt, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("Error: the key used to encrypt the content must be of 32 bits")
	}

	// AES cipher
	c, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	// Galois Counter Mode
	gcm, err := cipher.NewGCM(c)
	if err != nil {
		return nil, err
	}

	// Byte array the of size of the nonce
	nonce := make([]byte, gcm.NonceSize())
	// Populates our nonce with a cryptographically secure random sequence
	_, err = io.ReadFull(rand.Reader, nonce)
	if err != nil {
		return nil, err
	} 

	// Encrypt the text using Seal function
	encryptedContent := gcm.Seal(nonce, nonce, contentToEncrypt, nil)

	return encryptedContent, nil
}