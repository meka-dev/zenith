package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mekapi/trc/eztrc"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"zenith/debug"
	"zenith/store"

	"github.com/go-kit/log"
	"github.com/gorilla/mux"
)

type Proxy struct {
	router  *mux.Router
	manager ReverseProxyManager
	logger  log.Logger
}

func NewProxy(manager ReverseProxyManager, logger log.Logger) (*Proxy, error) {
	p := &Proxy{
		router:  mux.NewRouter(),
		manager: manager,
		logger:  logger,
	}

	p.router.StrictSlash(true)
	p.router.Methods("GET").Path("/-/ping").Name("GET /-/ping").HandlerFunc(p.handlePing)
	p.router.PathPrefix("/").Name("(proxy)").HandlerFunc(p.handleProxy)

	p.router.Use(
		// GunzipRequestMiddleware not needed because we get the Chain ID from the header
		corsHeadersMiddleware,
		debug.TracingMiddleware,
		debug.MetricsMiddleware,
		panicRecoveryMiddleware(logger), // should be after observability middlewares
		// the handler executes here
	)

	return p, nil
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.router.ServeHTTP(w, r)
}

func (p *Proxy) handlePing(w http.ResponseWriter, r *http.Request) {
	all := p.manager.GetAllProxies()
	if len(all) <= 0 {
		respondError(w, r, fmt.Errorf("no proxies configured"), http.StatusServiceUnavailable, p.logger)
		return
	}

	ctx := r.Context()
	eztrc.Tracef(ctx, "ping remote count %d", len(all))

	errc := make(chan error, len(all))
	for _, rp := range all {
		uri, proxy, req := rp.URI, rp.Proxy, r.Clone(ctx) // path `/-/ping` is the same
		go func() {
			rec := httptest.NewRecorder()
			proxy.ServeHTTP(rec, req)
			code := rec.Result().StatusCode
			eztrc.Tracef(ctx, "ping %s (%s): %d", uri, req.URL, code)
			switch code {
			case http.StatusOK:
				errc <- nil
			default:
				errc <- fmt.Errorf("%s: %d", req.URL, code)
			}
		}()
	}

	var errstrs []string
	for i := 0; i < cap(errc); i++ {
		if err := <-errc; err != nil {
			errstrs = append(errstrs, err.Error())
		}
	}
	if len(errstrs) > 0 {
		err := fmt.Errorf("%s", strings.Join(errstrs, "; "))
		respondError(w, r, err, http.StatusBadGateway, p.logger)
		return
	}

	respondOK(w, r, struct{}{})
}

const (
	ChainIDHeaderKey = "Zenith-Chain-Id"
	maxBodyBytes     = 100 * 1024 * 1024 // 100 MB
)

func (p *Proxy) handleProxy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// The root path is special, and redirects to the docs page.
	if r.URL.Path == "/" {
		redirectHandler.ServeHTTP(w, r)
		return
	}

	// Everything else gets proxied.
	chainID, err := readChainID(w, r)
	if err != nil {
		respondError(w, r, err, http.StatusBadRequest, p.logger)
		return
	}

	rp, ok := p.manager.GetByChainID(chainID)
	if !ok {
		respondError(w, r, fmt.Errorf("%s %q not found", ChainIDHeaderKey, chainID), http.StatusBadRequest, p.logger)
		return
	}

	eztrc.Tracef(ctx, "proxying %s -> %s", rp.ChainID, rp.URI)

	rp.Proxy.ServeHTTP(w, r.WithContext(ctx))
}

func readChainID(w http.ResponseWriter, r *http.Request) (string, error) {
	ctx := r.Context()

	if chainID := r.URL.Query().Get("chain_id"); chainID != "" {
		eztrc.Tracef(ctx, "chain ID %q from query param", chainID)
		return chainID, nil
	}

	if chainID := r.Header.Get(ChainIDHeaderKey); chainID != "" {
		eztrc.Tracef(ctx, "chain ID %q from request header", chainID)
		return chainID, nil
	}

	limitReader := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	bodyBytes, err := io.ReadAll(limitReader)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	if values, err := url.ParseQuery(string(bodyBytes)); err == nil {
		if chainID := values.Get("chain_id"); chainID != "" {
			eztrc.Tracef(ctx, "chain ID %q from www-urlencoded body", chainID)
			return chainID, nil
		}
	}

	var x struct {
		ChainID string `json:"chain_id"`
	}
	if err := json.Unmarshal(bodyBytes, &x); err == nil {
		if x.ChainID != "" {
			eztrc.Tracef(ctx, "chain ID %q from JSON body", x.ChainID)
			return x.ChainID, nil
		}
	}

	return "", fmt.Errorf("no chain ID in request")
}

func modifyResponse(resp *http.Response) error {
	ctx := resp.Request.Context()

	eztrc.Tracef(ctx, "%s: %d %s (%dB)", resp.Request.URL, resp.StatusCode, http.StatusText(resp.StatusCode), resp.ContentLength)

	for k, vs := range resp.Header {
		for _, v := range vs {
			eztrc.Tracef(ctx, "← %s: %s", k, v)
		}
	}

	return nil
}

//
//
//

type ReverseProxyManager interface {
	GetAllProxies() []*ReverseProxy
	GetByChainID(chainID string) (*ReverseProxy, bool)
}

type ReverseProxy struct {
	ChainID string
	URI     string
	Proxy   *httputil.ReverseProxy
}

type StoreReverseProxyManager struct {
	store        store.Store
	networkToURI map[string]string // immutable

	mtx       sync.Mutex
	byChainID map[string]*ReverseProxy // mutable
}

func NewStoreReverseProxyManager(store store.Store, networkToURI map[string]string) *StoreReverseProxyManager {
	return &StoreReverseProxyManager{
		store:        store,
		networkToURI: networkToURI,
		byChainID:    map[string]*ReverseProxy{},
	}
}

var _ ReverseProxyManager = (*StoreReverseProxyManager)(nil)

func (p *StoreReverseProxyManager) GetAllProxies() []*ReverseProxy {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	all := make([]*ReverseProxy, 0, len(p.byChainID))
	for _, rp := range p.byChainID {
		all = append(all, rp)
	}

	return all
}

func (p *StoreReverseProxyManager) GetByChainID(chainID string) (*ReverseProxy, bool) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	rp, ok := p.byChainID[chainID]
	return rp, ok
}

func (p *StoreReverseProxyManager) Refresh(ctx context.Context) error {
	chains, err := p.store.ListChains(ctx)
	if err != nil {
		return fmt.Errorf("list chains from store: %w", err)
	}

	nextgen := map[string]*ReverseProxy{} // uri: proxy
	for _, c := range chains {
		uri, ok := p.networkToURI[c.Network]
		if !ok {
			eztrc.Tracef(ctx, "ignoring chain ID %s, network %s", c.ID, c.Network)
			continue
		}

		if !strings.HasPrefix(uri, "http") {
			uri = "http://" + uri
		}

		u, err := url.Parse(uri)
		if err != nil {
			return fmt.Errorf("%s: %s: %w", c.ID, uri, err)
		}

		u.Path = ""
		uri = u.String()

		rp := httputil.NewSingleHostReverseProxy(u)
		rp.ModifyResponse = modifyResponse

		eztrc.Tracef(ctx, "proxy chain ID %s, network %s, via %s", c.ID, c.Network, uri)

		nextgen[c.ID] = &ReverseProxy{
			ChainID: c.ID,
			URI:     uri,
			Proxy:   rp,
		}
	}

	p.mtx.Lock()
	defer p.mtx.Unlock()

	for chainID := range p.byChainID {
		if _, ok := nextgen[chainID]; !ok {
			eztrc.Tracef(ctx, "dropped proxy chain ID %s", chainID)
		}
	}

	p.byChainID = nextgen

	return nil
}
