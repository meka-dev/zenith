package cryptoutil

import (
	"encoding/hex"
	"strings"

	"github.com/meka-dev/mekatek-go/mekabuild"
)

func HashTx(txb []byte) string {
	return strings.ToUpper(hex.EncodeToString(mekabuild.HashTxs(txb)))
}

func HashTxs(txbs [][]byte) []string {
	hashes := make([]string, len(txbs))
	for i, tx := range txbs {
		hashes[i] = HashTx(tx)
	}
	return hashes
}
