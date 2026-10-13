package webpush

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	saltLength       = 16
	authSecretLength = 16
	contentKeyLength = 16
	nonceLength      = 12
	// recordSize is the single record this encoder writes. It is the largest
	// record every push service accepts.
	recordSize = 4096
	// maxPlaintextBytes leaves room for the padding delimiter and the AES-GCM
	// authentication tag inside one record.
	maxPlaintextBytes = recordSize - 16 - 1
	// paddingDelimiter marks the last record of a payload (RFC 8188).
	paddingDelimiter = 0x02
)

// encryptRecord builds the aes128gcm body of one push message: RFC 8188
// framing around a record whose keys come from the RFC 8291 key agreement
// between the application server and the subscription. The salt and the
// ephemeral key are parameters so the RFC 8291 Appendix A vector can be
// reproduced exactly.
func encryptRecord(clientPublicKey, authSecret, plaintext, salt []byte, senderKey *ecdh.PrivateKey) ([]byte, error) {
	clientPublic, err := ecdh.P256().NewPublicKey(clientPublicKey)
	if err != nil {
		return nil, fmt.Errorf("client public key: %w", err)
	}
	if len(authSecret) != authSecretLength {
		return nil, fmt.Errorf("client auth secret must be %d bytes", authSecretLength)
	}
	if len(salt) != saltLength {
		return nil, fmt.Errorf("salt must be %d bytes", saltLength)
	}
	if len(plaintext) > maxPlaintextBytes {
		return nil, errors.New("push payload does not fit in one record")
	}
	sharedSecret, err := senderKey.ECDH(clientPublic)
	if err != nil {
		return nil, err
	}
	senderPublicKey := senderKey.PublicKey().Bytes()
	contentKey, nonce, err := deriveContentKeys(clientPublicKey, senderPublicKey, sharedSecret, authSecret, salt)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(contentKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	record := make([]byte, 0, len(plaintext)+1)
	record = append(record, plaintext...)
	record = append(record, paddingDelimiter)
	ciphertext := aead.Seal(nil, nonce, record, nil)

	body := make([]byte, 0, saltLength+5+len(senderPublicKey)+len(ciphertext))
	body = append(body, salt...)
	body = binary.BigEndian.AppendUint32(body, recordSize)
	body = append(body, byte(len(senderPublicKey)))
	body = append(body, senderPublicKey...)
	return append(body, ciphertext...), nil
}

// deriveContentKeys runs the two HKDF stages of RFC 8291 section 3.4: the
// subscription's auth secret combines the two public keys into the input
// keying material, and the message salt turns that into the record's content
// encryption key and nonce.
func deriveContentKeys(clientPublicKey, senderPublicKey, sharedSecret, authSecret, salt []byte) (contentKey, nonce []byte, err error) {
	keyInfo := make([]byte, 0, len("WebPush: info")+1+len(clientPublicKey)+len(senderPublicKey))
	keyInfo = append(keyInfo, "WebPush: info"...)
	keyInfo = append(keyInfo, 0)
	keyInfo = append(keyInfo, clientPublicKey...)
	keyInfo = append(keyInfo, senderPublicKey...)
	combiningKey, err := hkdf.Extract(sha256.New, sharedSecret, authSecret)
	if err != nil {
		return nil, nil, err
	}
	inputKeyingMaterial, err := hkdf.Expand(sha256.New, combiningKey, string(keyInfo), sha256.Size)
	if err != nil {
		return nil, nil, err
	}
	contentPRK, err := hkdf.Extract(sha256.New, inputKeyingMaterial, salt)
	if err != nil {
		return nil, nil, err
	}
	contentKey, err = hkdf.Expand(sha256.New, contentPRK, "Content-Encoding: aes128gcm\x00", contentKeyLength)
	if err != nil {
		return nil, nil, err
	}
	nonce, err = hkdf.Expand(sha256.New, contentPRK, "Content-Encoding: nonce\x00", nonceLength)
	if err != nil {
		return nil, nil, err
	}
	return contentKey, nonce, nil
}

func newSalt() ([]byte, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return salt, nil
}
