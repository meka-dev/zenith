package block

import (
	"context"
	"fmt"
	"mekapi/trc/eztrc"
	"sync"

	"zenith/chain"
	"zenith/store"
)

type AllowChainFunc func(*store.Chain) bool

type ConvertChainFunc func(*store.Chain) (chain.Chain, error)

type CreateServiceFunc func(chain.Chain, store.Store) Service

type ServiceManager struct {
	store   store.Store // can be nil if manager is constructed as static
	allow   AllowChainFunc
	convert ConvertChainFunc
	create  CreateServiceFunc

	mtx     sync.Mutex
	managed map[string]Service
}

func NewServiceManager(store store.Store, allow AllowChainFunc, convert ConvertChainFunc, create CreateServiceFunc) *ServiceManager {
	return &ServiceManager{
		store:   store,
		allow:   allow,
		convert: convert,
		create:  create,
	}
}

func NewStaticServiceManager(services ...Service) *ServiceManager {
	managed := make(map[string]Service, len(services))
	for _, s := range services {
		managed[s.ChainID()] = s
	}
	return &ServiceManager{
		managed: managed,
	}
}

func (m *ServiceManager) Refresh(ctx context.Context) error {
	if m.store == nil {
		return fmt.Errorf("refresh on static service manager (no store)")
	}

	storeChains, err := m.store.ListChains(ctx)
	if err != nil {
		return fmt.Errorf("list chains from store: %w", err)
	}

	var chainChains []chain.Chain
	for _, sc := range storeChains {
		if !m.allow(sc) {
			eztrc.Tracef(ctx, "store chain ID %q: ignored", sc.ID)
			continue
		}
		cc, err := m.convert(sc)
		if err != nil {
			eztrc.Errorf(ctx, "store chain ID %q: error: %v", sc.ID, err)
			return fmt.Errorf("convert chain ID %s: %w", sc.ID, err)
		}
		eztrc.Tracef(ctx, "store chain ID %q: accepted", sc.ID)
		chainChains = append(chainChains, cc)
	}

	m.mtx.Lock()
	defer m.mtx.Unlock()

	// Ensure the services in the manager are 1-to-1 with the chains fetched
	// from the store. Do that by creating a "next gen" set of services.
	nextgen := map[string]Service{}

	// Every chain we got from the store should have a managed service.
	for _, cc := range chainChains {
		// Ideally, if we already had a service under management, we wouldn't
		// re-create it unless some of its properties had changed. But it's a
		// little tricky to figure out when that happens, as our abstractions
		// obscure the relevant information.
		//
		// TODO: don't re-create services if their parameters haven't changed
		id := cc.ID()
		if _, ok := m.managed[id]; ok {
			eztrc.Tracef(ctx, "%s: update existing service", id)
			nextgen[id] = m.create(cc, m.store)
			delete(m.managed, id)
		} else {
			eztrc.Tracef(ctx, "%s: create new service", id)
			nextgen[id] = m.create(cc, m.store)
		}
	}

	// Anything left over should be removed.
	for id := range m.managed {
		eztrc.Tracef(ctx, "%s: remove dropped service", id)
		delete(m.managed, id)
	}

	m.managed = nextgen
	return nil
}

func (m *ServiceManager) GetService(chainID string) (Service, bool) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	s, ok := m.managed[chainID]
	return s, ok
}

func (m *ServiceManager) AllServices() []Service {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	all := make([]Service, 0, len(m.managed))
	for _, s := range m.managed {
		all = append(all, s)
	}
	return all
}
