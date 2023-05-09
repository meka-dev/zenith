package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"mekapi/trc/eztrc"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"
	"zenith/api"
	"zenith/build"
	"zenith/debug"
	"zenith/store"
	"zenith/store/memstore"
	"zenith/store/pgstore"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/oklog/run"
	"github.com/peterbourgon/ff/v3"
)

func main() {
	err := exe(os.Stdout, os.Stderr, os.Args[1:])
	switch {
	case err == nil:
		os.Exit(0)
	case errors.Is(err, flag.ErrHelp):
		os.Exit(0)
	case isSignalError(err):
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(0)
	case err != nil:
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func exe(stdout, stderr io.Writer, args []string) error {
	fs := flag.NewFlagSet("zenith-proxy", flag.ContinueOnError)
	var (
		ctx                  = context.Background()
		proxyAddr            = fs.String("proxy-addr", ":4411", "public proxy HTTP server address")
		debugAddr            = fs.String("debug-addr", ":4412", "private debug HTTP server address")
		storeConnStr         = fs.String("store-conn-str", "mem://store", "store connection string")
		networks             = repeatedString(fs, "network", "<network>:<uri> e.g. 'osmosis:localhost:4412' (repeatable)")
		chainRefreshInterval = fs.Duration("chain-refresh-interval", 1*time.Minute, "how often to fetch chain IDs from the store")
		version              = fs.Bool("version", false, "print version information and exit")
		logLevel             = fs.String("log-level", "info", "debug, info, warn, error")
		_                    = fs.String("config", "", "config file")
	)
	if err := ff.Parse(fs, args,
		ff.WithConfigFileFlag("config"),
		ff.WithConfigFileParser(ff.PlainParser),
		ff.WithEnvVarPrefix("ZENITH"),
	); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	if *version {
		fmt.Fprintf(stdout, "zenith-proxy version %s date %s\n", build.Version, build.Date)
		return nil
	}

	var logger log.Logger
	{
		logger = log.NewLogfmtLogger(stderr)
		logger = level.NewFilter(logger, level.Allow(level.ParseDefault(*logLevel, level.InfoValue())))

		level.Info(logger).Log("build_version", build.Version, "build_date", build.Date)
	}

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

	level.Debug(logger).Log("msg", "parsing -network flags")

	networkToURI := map[string]string{}
	{
		for _, n := range networks.get() {
			network, uri, ok := strings.Cut(n, ":")
			if !ok || network == "" || uri == "" {
				return fmt.Errorf("invalid -network %q", n)
			}
			if existing, ok := networkToURI[network]; ok {
				return fmt.Errorf("duplicate -network %s URIs (%s, %s)", network, existing, uri)
			}
			networkToURI[network] = uri
			level.Info(logger).Log("network", network, "uri", uri)
		}
	}

	level.Debug(logger).Log("msg", "constructing reverse proxy manager from networks")

	var manager *api.StoreReverseProxyManager
	{
		p := api.NewStoreReverseProxyManager(st, networkToURI)
		if err := p.Refresh(ctx); err != nil {
			return fmt.Errorf("initial refresh of reverse proxies: %w", err)
		}
		manager = p
	}

	level.Debug(logger).Log("msg", "constructing proxy handler")

	var proxyHandler http.Handler
	{
		p, err := api.NewProxy(manager, logger)
		if err != nil {
			return fmt.Errorf("create proxy: %w", err)
		}
		proxyHandler = p
	}

	level.Debug(logger).Log("msg", "starting up")

	var g run.Group

	{
		logger := log.With(logger, "module", "proxy")
		server := &http.Server{Handler: proxyHandler, Addr: *proxyAddr}
		g.Add(func() error {
			level.Info(logger).Log("proxy_addr", *proxyAddr)
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
		logger := log.With(logger, "module", "chain_refresh")
		ctx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			level.Info(logger).Log("interval", *chainRefreshInterval)
			ticker := time.NewTicker(*chainRefreshInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					ctx, finish := eztrc.Create(ctx, "refresh chains")
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
		g.Add(run.SignalHandler(ctx, syscall.SIGINT, syscall.SIGTERM))
	}

	level.Debug(logger).Log("msg", "running")

	return g.Run()
}

func isSignalError(err error) bool {
	var (
		sigErrVal run.SignalError
		sigErrPtr *run.SignalError
	)
	return errors.As(err, &sigErrVal) || errors.As(err, &sigErrPtr)
}

func repeatedString(fs *flag.FlagSet, name string, usage string) *stringSet {
	var ss stringSet
	fs.Var(&ss, name, usage)
	return &ss
}

type stringSet struct {
	s []string
	m map[string]struct{}
}

func (s *stringSet) Set(v string) error {
	if s.m == nil {
		s.m = map[string]struct{}{}
	}
	if _, ok := s.m[v]; !ok {
		s.m[v] = struct{}{}
		s.s = append(s.s, v)
	}
	return nil
}

func (s *stringSet) String() string {
	return strings.Join(s.s, " ")
}

func (s *stringSet) get() []string {
	return s.s
}
