package crypto

import (
	"crypto/sha512"
)

// Digest computes the SHA-512 hash of the input data.
// It returns a 64-byte array, consistent with production-grade security standards.
func Digest(data string) [64]byte {
	return sha512.Sum512([]byte(data))
}
