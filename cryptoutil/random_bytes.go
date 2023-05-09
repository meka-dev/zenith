package cryptoutil

import (
	"crypto/rand"
	"fmt"
)

func RandomBytes(n int) []byte {
	b := make([]byte, n)
	nn, err := rand.Read(b)
	if err != nil {
		panic(fmt.Errorf("get random bytes: %v", err))
	}
	if nn != n {
		panic(fmt.Errorf("short read: %d < %d", nn, n))
	}
	return b
}
