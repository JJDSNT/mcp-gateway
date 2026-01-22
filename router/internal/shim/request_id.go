package shim

import (
	"crypto/rand"
	"encoding/hex"
)

// NewRequestID gera um id curto e seguro (16 bytes -> 32 hex).
func NewRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
