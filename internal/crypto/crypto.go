package crypto

import (
	"crypto/ed25519"
	"crypto/sha512"
	"fmt"
)

// Digest computes the SHA-512 hash of the input data.
// It returns a 64-byte array, consistent with production-grade security standards.
func Digest(data string) [64]byte {
	return sha512.Sum512([]byte(data))
}

// GenerateKeyPair creates a new Ed25519 key pair for a node.
func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(nil)
}

// Sign creates a digital signature for a message using a private key.
func Sign(privateKey ed25519.PrivateKey, message []byte) []byte {
	return ed25519.Sign(privateKey, message)
}

// Verify checks if a digital signature is valid for a given message and public key.
func Verify(publicKey ed25519.PublicKey, message []byte, signature []byte) bool {
	if len(signature) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(publicKey, message, signature)
}

// LoadPublicKey converts a raw byte slice to an ed25519.PublicKey.
func LoadPublicKey(raw []byte) (ed25519.PublicKey, error) {
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key size: %d", len(raw))
	}
	return ed25519.PublicKey(raw), nil
}
