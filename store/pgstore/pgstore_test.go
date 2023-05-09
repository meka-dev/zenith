package pgstore_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"zenith/store"
	"zenith/store/pgstore"
	"zenith/store/storetest"
)

func TestStore(t *testing.T) {
	t.Parallel()

	if os.Getenv("PGCONNSTRING") == "" {
		t.Skipf("set PGCONNSTRING to run this test")
	}

	storetest.TestStore(t, pgstore.NewTestStore)
}

func TestPGStoreTransactionIsolation(t *testing.T) {
	t.Parallel()

	if os.Getenv("PGCONNSTRING") == "" {
		t.Skipf("set PGCONNSTRING to run this test")
	}

	var (
		ctx     = context.Background()
		store1  = pgstore.NewTestStore(t)
		store2  = pgstore.NewTestStore(t)
		chain   = storetest.NewChain(t, store1)
		oldaddr = chain.MekatekPaymentAddress
		newaddr = oldaddr + "_new"
	)

	runtx := func(st store.Store, stepch <-chan int) error {
		t.Logf("step %d", <-stepch)

		return st.Transact(ctx, func(tx store.Store) error {
			c, err := tx.SelectChain(ctx, chain.ID)
			if err != nil {
				return fmt.Errorf("SelectChain: %w", err)
			}

			t.Logf("step %d", <-stepch)

			switch c.MekatekPaymentAddress {
			case oldaddr:
				c.MekatekPaymentAddress = newaddr
			case newaddr:
				return fmt.Errorf("payment addr already updated to %s", newaddr)
			default:
				return fmt.Errorf("bonkers payment addr %s", c.MekatekPaymentAddress)
			}

			t.Logf("step %d", <-stepch)

			if err := tx.UpsertChain(ctx, c); err != nil {
				return fmt.Errorf("upsert failed: %w", err)
			}

			return nil
		})
	}

	var (
		stepc1 = make(chan int, 100)
		errc1  = make(chan error, 1)
	)
	go func() { errc1 <- runtx(store1, stepc1) }()

	var (
		stepc2 = make(chan int, 100)
		errc2  = make(chan error, 1)
	)
	go func() { errc2 <- runtx(store2, stepc2) }()

	stepc1 <- 1     // allow store1 to enter Transact
	stepc2 <- 2     // allow store2 to enter Transact
	stepc1 <- 3     // allow store1 to update payment address
	stepc1 <- 4     // allow store1 to upsert chain
	err1 := <-errc1 // store1 should successfully Transact
	stepc2 <- 5     // allow store2 to update payment address -- this can be OK
	stepc2 <- 6     // allow store2 to upsert chain -- this should probably fail
	err2 := <-errc2 // store2 should fail to Transact

	if err1 != nil {
		t.Errorf("store1 should have successfully transacted, but had error: %v", err1)
	}

	if err2 == nil {
		t.Errorf("store2 should have failed to transact, but succeeded")
	}
}
