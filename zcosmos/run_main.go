package zcosmos

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"mekapi/trc/eztrc"
	"net/http"
	"strings"
	"syscall"
	"time"
	"zenith/api"
	"zenith/block"
	"zenith/build"
	"zenith/chain"
	"zenith/debug"
	"zenith/store"
	"zenith/store/memstore"
	"zenith/store/pgstore"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/oklog/run"
	"github.com/peterbourgon/ff/v3"
)

type RunConfig struct {
	Program   string    // e.g. "zenith-osmosis"
	Stdout    io.Writer // e.g. os.Stdout
	Stderr    io.Writer // e.g. os.Stderr
	Args      []string  // e.g. os.Args
	APIAddr   string    // e.g. ":4417"
	DebugAddr string    // e.g. ":4418"

	NetworkConfig
}

func (cfg *RunConfig) Validate() error {
	if cfg.Program == "" {
		return fmt.Errorf("missing program name")
	}
	if cfg.Stdout == nil {
		cfg.Stdout = io.Discard
	}
	if cfg.Stderr == nil {
		cfg.Stderr = io.Discard
	}
	if cfg.APIAddr == "" {
		return fmt.Errorf("missing API addr")
	}
	if cfg.DebugAddr == "" {
		return fmt.Errorf("missing debug addr")
	}
	if cfg.Network == "" {
		cfg.Network = build.Network
	}
	if cfg.Bech32PrefixAccAddr == "" {
		return fmt.Errorf("missing bech32 prefix")
	}
	if cfg.StallThreshold == 0 {
		cfg.StallThreshold = 5 * time.Minute
	}
	if cfg.Codec == nil {
		return fmt.Errorf("missing codec")
	}
	if cfg.TxConfig == nil {
		return fmt.Errorf("missing tx config")
	}
	return nil
}

func RunMain(ctx context.Context, cfg RunConfig) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	fs := flag.NewFlagSet(cfg.Program, flag.ContinueOnError)
	var (
		apiAddr                = fs.String("api-addr", cfg.APIAddr, "public API HTTP server address")
		debugAddr              = fs.String("debug-addr", cfg.DebugAddr, "private debug HTTP server address")
		storeConnStr           = fs.String("store-conn-str", "mem://store", "store connection string")
		storeCleanupInterval   = fs.Duration("store-cleanup-interval", time.Minute, "how often to clean up the store")
		storeMetricsInterval   = fs.Duration("store-metrics-interval", 10*time.Second, "how often to update store metrics")
		serviceRefreshInterval = fs.Duration("service-refresh-interval", 1*time.Minute, "how often to refresh services from chain data in store")
		overrideNodes          = flagStringSet(fs, "override-node", "if set, override store node URIs, format '<chain ID>:<URI>' (optional, repeatable)")
		version                = fs.Bool("version", false, "print version information and exit")
		logLevel               = fs.String("log-level", "info", "debug, info, warn, error")
		_                      = fs.String("config", "", "config file")
	)
	if err := ff.Parse(fs, cfg.Args,
		ff.WithConfigFileFlag("config"),
		ff.WithConfigFileParser(ff.PlainParser),
		ff.WithEnvVarPrefix("ZENITH"),
	); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	if *version {
		fmt.Fprintf(cfg.Stdout, "%s version %s date %s\n", cfg.Program, build.Version, build.Date)
		return nil
	}

	var logger log.Logger
	{
		logger = log.NewLogfmtLogger(cfg.Stderr)
		logger = level.NewFilter(logger, level.Allow(level.ParseDefault(*logLevel, level.InfoValue())))
	}

	level.Info(logger).Log("program", cfg.Program, "network", cfg.Network, "build_version", build.Version, "build_date", build.Date)

	level.Debug(logger).Log("msg", "creating store")

	var st store.Store
	{
		switch {
		case strings.HasPrefix(*storeConnStr, "postgres"):
			level.Info(logger).Log("store", "postgres")
			s, err := pgstore.NewStore(ctx, *storeConnStr, log.With(logger, "module", "store"))
			if err != nil {
				return fmt.Errorf("create Postgres store: %w", err)
			}
			defer func() {
				level.Debug(logger).Log("msg", "closing Postgres store")
				if err := s.Close(); err != nil {
					level.Error(logger).Log("msg", "close Postgres store failed", "err", err)
				}
			}()
			st = s

		default:
			level.Warn(logger).Log("store", "in-memory")
			st = memstore.NewStore()
		}
	}

	level.Debug(logger).Log("msg", "listing chains")

	storeChains, err := st.ListChains(ctx)
	if err != nil {
		return fmt.Errorf("list chains: %w", err)
	}

	level.Debug(logger).Log("msg", "fetched all chains from store", "count", len(storeChains))

	// This was added during infrastructure re-provisioning, to allow us to
	// deploy a set of Zenith API instances that used different full nodes,
	// without impacting production.
	//
	// IMO this suggests that full node URIs probably shouldn't be stored in the
	// store, and should be moved to instance config permanently.
	var maybeOverrideFullNodeURIs func(*store.Chain)
	{
		overrides := map[string][]string{}
		for _, s := range overrideNodes.Get() {
			chainID, nodeURI, ok := strings.Cut(s, ":")
			if !ok || chainID == "" || nodeURI == "" {
				continue
			}
			overrides[chainID] = append(overrides[chainID], nodeURI)
		}

		for chainID, nodeURIs := range overrides {
			level.Warn(logger).Log("msg", "overriding full node URIs for chain", "chain_id", chainID, "node_uris", strings.Join(nodeURIs, ", "))
		}

		maybeOverrideFullNodeURIs = func(sc *store.Chain) {
			nodeURIs, ok := overrides[sc.ID]
			if !ok {
				return
			}
			level.Warn(logger).Log(
				"msg", "overriding full node URIs for chain",
				"chain_id", sc.ID,
				"old", strings.Join(sc.NodeURIs, ", "),
				"new", strings.Join(nodeURIs, ", "),
			) // TOOD: maybe too spammy
			sc.NodeURIs = nodeURIs
		}
	}

	var manager *block.ServiceManager
	{
		allow := func(sc *store.Chain) bool {
			return sc.Network == cfg.Network
		}

		convert := func(sc *store.Chain) (chain.Chain, error) {
			maybeOverrideFullNodeURIs(sc) // TODO: maybe remove eventually?
			cc, err := NewChain(
				cfg.NetworkConfig,
				sc.ID,
				sc.NodeURIs,
				&http.Client{Timeout: sc.Timeout},
			)
			if err != nil {
				return nil, fmt.Errorf("create chain: %w", err)
			}
			if err := cc.ValidatePaymentAddress(ctx, sc.MekatekPaymentAddress); err != nil {
				return nil, fmt.Errorf("payment address (%s): %w", sc.MekatekPaymentAddress, err)
			}
			return chain.WithRingCache(cc), nil
		}

		create := func(c chain.Chain, s store.Store) block.Service {
			return block.NewCoreService(c, s)
		}

		m := block.NewServiceManager(st, allow, convert, create)
		if err := m.Refresh(ctx); err != nil {
			return fmt.Errorf("initial refresh of services: %w", err)
		}

		manager = m
	}

	for _, s := range manager.AllServices() {
		level.Info(logger).Log("msg", "added chain", "chain_id", s.ChainID())
	}

	var g run.Group

	{
		logger := log.With(logger, "module", "api")
		apiHandler := api.NewHandler(st, manager, logger)
		server := &http.Server{Handler: apiHandler, Addr: *apiAddr}
		g.Add(func() error {
			level.Info(logger).Log("api_addr", *apiAddr)
			return server.ListenAndServe()
		}, func(error) {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			server.Shutdown(ctx)
		})
	}

	{
		logger := log.With(logger, "module", "debug")
		debugHandler := debug.NewHandler()
		server := &http.Server{Handler: debugHandler, Addr: *debugAddr}
		g.Add(func() error {
			level.Info(logger).Log("debug_addr", *debugAddr)
			return server.ListenAndServe()
		}, func(error) {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			server.Shutdown(ctx)
		})
	}

	{
		logger := log.With(logger, "module", "store_cleanup")
		ctx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			level.Info(logger).Log("interval", *storeCleanupInterval)
			ticker := time.NewTicker(*storeCleanupInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					ctx, finish := eztrc.Create(ctx, "store cleanup")
					if err := st.Cleanup(ctx); err != nil {
						eztrc.Errorf(ctx, "failed: %v", err)
						level.Error(logger).Log("error", err)
					}
					finish()
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}, func(error) {
			cancel()
		})
	}

	{
		logger := log.With(logger, "module", "store_metrics")
		ctx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			level.Info(logger).Log("interval", *storeMetricsInterval)
			ticker := time.NewTicker(*storeMetricsInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					ctx, finish := eztrc.Create(ctx, "store metrics")
					if err := store.UpdateMetrics(ctx, st); err != nil {
						eztrc.Errorf(ctx, "failed: %v", err)
						level.Error(logger).Log("error", err)
					}
					finish()
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}, func(error) {
			cancel()
		})
	}

	{
		logger := log.With(logger, "module", "service_refresh")
		ctx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			level.Info(logger).Log("interval", *serviceRefreshInterval)
			ticker := time.NewTicker(*serviceRefreshInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					ctx, finish := eztrc.Create(ctx, "refresh services")
					if err := manager.Refresh(ctx); err != nil {
						eztrc.Errorf(ctx, "failed: %v", err)
						level.Error(logger).Log("error", err)
					}
					finish()
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}, func(error) {
			cancel()
		})
	}

	{
		g.Add(run.SignalHandler(context.Background(), syscall.SIGINT, syscall.SIGTERM))
	}

	level.Debug(logger).Log("msg", "running")

	return g.Run()
}

func IsSignalError(err error) bool {
	var (
		sigErrVal run.SignalError
		sigErrPtr *run.SignalError
	)
	return errors.As(err, &sigErrVal) || errors.As(err, &sigErrPtr)
}

//
//
//

type stringSet struct{ values []string }

var _ flag.Value = (*stringSet)(nil)

func flagStringSet(fs *flag.FlagSet, name string, usage string) *stringSet {
	ss := &stringSet{}
	fs.Var(ss, name, usage)
	return ss
}

func (ss *stringSet) Set(value string) error {
	for _, v := range ss.values {
		if value == v {
			return nil
		}
	}
	ss.values = append(ss.values, value)
	return nil
}

func (ss *stringSet) String() string {
	switch len(ss.values) {
	case 0:
		return "<empty>"
	default:
		return strings.Join(ss.values, ", ")
	}
}

func (ss *stringSet) Get() []string {
	return ss.values
}
