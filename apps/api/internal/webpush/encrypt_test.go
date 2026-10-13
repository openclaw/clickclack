package webpush

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"
)

// RFC 8291 Appendix A, "Push Message Encryption Example". Every value here is
// the one printed in the RFC, with the line wrapping removed.
const (
	vectorPlaintext        = "When I grow up, I want to be a watermelon"
	vectorClientPublicKey  = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
	vectorClientPrivateKey = "q1dXpw3UpT5VOmu_cf_v6ih07Aems3njxI-JWgLcM94"
	vectorSenderPublicKey  = "BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8"
	vectorSenderPrivateKey = "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"
	vectorAuthSecret       = "BTBZMqHH6r4Tts7J_aSIgg"
	vectorSalt             = "DGv6ra1nlYgDCS1FRnbzlw"
	vectorSharedSecret     = "kyrL1jIIOHEzg3sM2ZWRHDRB62YACZhhSlknJ672kSs"
	vectorContentKey       = "oIhVW04MRdy2XN9CiKLxTg"
	vectorNonce            = "4h_95klXJ5E_qnoN"
	vectorCiphertext       = "8pfeW0KbunFT06SuDKoJH9Ql87S1QUrdirN6GcG7sFz1y1sqLgVi1VhjVkHsUoEsbI_0LpXMuGvnzQ"
)

func TestEncryptRecordMatchesRFC8291Vector(t *testing.T) {
	t.Parallel()
	clientPublicKey := decodeVector(t, vectorClientPublicKey)
	senderPublicKey := decodeVector(t, vectorSenderPublicKey)
	authSecret := decodeVector(t, vectorAuthSecret)
	salt := decodeVector(t, vectorSalt)
	senderKey, err := ecdh.P256().NewPrivateKey(decodeVector(t, vectorSenderPrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	clientPublic, err := ecdh.P256().NewPublicKey(clientPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	sharedSecret, err := senderKey.ECDH(clientPublic)
	if err != nil {
		t.Fatal(err)
	}
	if got := encodeVector(sharedSecret); got != vectorSharedSecret {
		t.Fatalf("shared secret %q, want %q", got, vectorSharedSecret)
	}
	contentKey, nonce, err := deriveContentKeys(clientPublicKey, senderPublicKey, sharedSecret, authSecret, salt)
	if err != nil {
		t.Fatal(err)
	}
	if got := encodeVector(contentKey); got != vectorContentKey {
		t.Fatalf("content encryption key %q, want %q", got, vectorContentKey)
	}
	if got := encodeVector(nonce); got != vectorNonce {
		t.Fatalf("nonce %q, want %q", got, vectorNonce)
	}

	body, err := encryptRecord(clientPublicKey, authSecret, []byte(vectorPlaintext), salt, senderKey)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body[:saltLength], salt) {
		t.Fatalf("body does not start with the salt: %x", body[:saltLength])
	}
	if got := binary.BigEndian.Uint32(body[saltLength : saltLength+4]); got != recordSize {
		t.Fatalf("record size %d, want %d", got, recordSize)
	}
	if got := int(body[saltLength+4]); got != len(senderPublicKey) {
		t.Fatalf("key length %d, want %d", got, len(senderPublicKey))
	}
	keyStart := saltLength + 5
	if !bytes.Equal(body[keyStart:keyStart+len(senderPublicKey)], senderPublicKey) {
		t.Fatal("body does not carry the sender public key")
	}
	if got := encodeVector(body[keyStart+len(senderPublicKey):]); got != vectorCiphertext {
		t.Fatalf("ciphertext %q, want %q", got, vectorCiphertext)
	}
}

// TestEncryptRecordRoundTrip runs a fresh salt and ephemeral key through the
// receiver's half of RFC 8291, which is the check the Appendix A vector cannot
// make: that a real subscription can open what the sender wrote.
func TestEncryptRecordRoundTrip(t *testing.T) {
	t.Parallel()
	clientKey, err := ecdh.P256().NewPrivateKey(decodeVector(t, vectorClientPrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	authSecret := decodeVector(t, vectorAuthSecret)
	senderKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	salt, err := newSalt()
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte(strings.Repeat("payload ", 16))
	body, err := encryptRecord(clientKey.PublicKey().Bytes(), authSecret, plaintext, salt, senderKey)
	if err != nil {
		t.Fatal(err)
	}
	decrypted := decryptRecord(t, clientKey, authSecret, body)
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("round trip produced %q", decrypted)
	}
}

func TestEncryptRecordRejectsBadInputs(t *testing.T) {
	t.Parallel()
	clientPublicKey := decodeVector(t, vectorClientPublicKey)
	authSecret := decodeVector(t, vectorAuthSecret)
	salt := decodeVector(t, vectorSalt)
	senderKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	offCurve := append([]byte(nil), clientPublicKey...)
	offCurve[len(offCurve)-1] ^= 0xff
	for name, params := range map[string]struct {
		clientPublicKey []byte
		authSecret      []byte
		plaintext       []byte
		salt            []byte
	}{
		"off curve client key": {offCurve, authSecret, []byte("hello"), salt},
		"short client key":     {clientPublicKey[:10], authSecret, []byte("hello"), salt},
		"short auth secret":    {clientPublicKey, authSecret[:8], []byte("hello"), salt},
		"short salt":           {clientPublicKey, authSecret, []byte("hello"), salt[:4]},
		"payload too large":    {clientPublicKey, authSecret, make([]byte, maxPlaintextBytes+1), salt},
	} {
		if _, err := encryptRecord(params.clientPublicKey, params.authSecret, params.plaintext, params.salt, senderKey); err == nil {
			t.Fatalf("expected %s to fail", name)
		}
	}
}

// decryptRecord is the receiver side of RFC 8291, used only to prove the
// sender side round trips.
func decryptRecord(t *testing.T, clientKey *ecdh.PrivateKey, authSecret, body []byte) []byte {
	t.Helper()
	salt := body[:saltLength]
	keyLength := int(body[saltLength+4])
	senderPublicKey := body[saltLength+5 : saltLength+5+keyLength]
	ciphertext := body[saltLength+5+keyLength:]
	senderPublic, err := ecdh.P256().NewPublicKey(senderPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	sharedSecret, err := clientKey.ECDH(senderPublic)
	if err != nil {
		t.Fatal(err)
	}
	contentKey, nonce, err := deriveContentKeys(clientKey.PublicKey().Bytes(), senderPublicKey, sharedSecret, authSecret, salt)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(contentKey)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	record, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(record) == 0 || record[len(record)-1] != paddingDelimiter {
		t.Fatalf("record is not delimited: %x", record)
	}
	return record[:len(record)-1]
}

func decodeVector(t *testing.T, value string) []byte {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func encodeVector(value []byte) string {
	return base64.RawURLEncoding.EncodeToString(value)
}
