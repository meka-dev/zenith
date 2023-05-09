package memstore_test

import (
	"testing"

	"zenith/store"
	"zenith/store/memstore"
	"zenith/store/storetest"
)

func TestStore(t *testing.T) {
	storetest.TestStore(t, func(t *testing.T) store.Store { return memstore.NewStore() })
}
