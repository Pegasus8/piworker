package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
)

//! This internal tool was created with this help:
//! https://tutorialedge.net/golang/go-encrypt-decrypt-aes-tutorial/

// DescryptContent is a function used to decrypt []byte
// content encrypted with AES encryption
func DescryptContent(contentToDecrypt, key []byte) (decryptedContent []byte, err error) {
	if len(key) != 32 {
		return nil, errors.New("Error: the key used to decrypt the content must be of 32 bits")
	}

	c, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(c)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(contentToDecrypt) < nonceSize {
		err = errors.New("Error: the length of the content to decrypt" +
			" is lower than the length of the nonce.")
		return nil, err
	}

	nonce, contentToDecrypt := contentToDecrypt[:nonceSize], contentToDecrypt[nonceSize:]
	decryptedContent, err = gcm.Open(nil, nonce, contentToDecrypt, nil)
	if err != nil {
		return nil, err
	}

	return decryptedContent, nil
}
