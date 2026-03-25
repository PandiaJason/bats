package crypto

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
)

func Hash(data []byte) [32]byte {
	return sha256.Sum256(data)
}

func BuildSigData(version uint8, seq uint64, sender [32]byte, hash [32]byte, payload []byte) []byte {
	buf := make([]byte, 1+8+32+32+len(payload))

	buf[0] = version
	binary.BigEndian.PutUint64(buf[1:9], seq)
	copy(buf[9:41], sender[:])
	copy(buf[41:73], hash[:])
	copy(buf[73:], payload)

	return buf
}

func Sign(priv ed25519.PrivateKey, data []byte) []byte {
	return ed25519.Sign(priv, data)
}

func Verify(pub ed25519.PublicKey, data, sig []byte) bool {
	return ed25519.Verify(pub, data, sig)
}
