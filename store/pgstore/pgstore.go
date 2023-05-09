package pgstore

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"mekapi/trc/eztrc"
	"time"

	"github.com/jackc/pgtype"

	"zenith/metrics"
	"zenith/store"
	"zenith/store/pgstore/migrations"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gofrs/uuid"
	"github.com/jackc/pgconn"
	pgx "github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/jackc/tern/migrate"
	"github.com/prometheus/client_golang/prometheus"
)

type Store struct {
	db     connOrTx
	logger log.Logger
}

var _ store.Store = (*Store)(nil)

type connOrTx interface {
	Query(ctx context.Context, q string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, q string, args ...any) pgx.Row
	Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error)
}

func NewStore(ctx context.Context, connStr string, logger log.Logger) (_ *Store, err error) {
	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("parse connection string: %w", err)
	}

	if config.MaxConnIdleTime == 0 {
		config.MaxConnIdleTime = 5 * time.Minute
	}

	if config.MaxConns == 0 {
		config.MaxConns = 4
	}

	if config.MinConns == 0 {
		config.MinConns = 1
	}

	if config.ConnConfig.ConnectTimeout == 0 {
		config.ConnConfig.ConnectTimeout = 5 * time.Second
	}

	config.ConnConfig.Logger = &pgDebugLogAdapter{
		Logger: log.With(logger, "submodule", "postgres"),
	}

	config.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		level.Debug(logger).Log("event", "new db connection")

		for _, q := range []string{
			`set timezone='UTC'`,
			`set lock_timeout='5s'`,
			`set statement_timeout='5s'`,
		} {
			if _, err := c.Exec(ctx, q); err != nil {
				return fmt.Errorf("db connection setup query %q: %w", q, err)
			}
		}

		return nil
	}

	level.Debug(logger).Log("msg", "connecting")

	pool, err := pgxpool.ConnectConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}

	{
		var (
			user = config.ConnConfig.User
			host = config.ConnConfig.Host
			name = config.ConnConfig.Database
			fn   = func() stat { return pool.Stat() }
			pc   = newPoolCollector(user, host, name, fn)
		)
		if err := prometheus.Register(pc); err != nil {
			return nil, fmt.Errorf("metrics registration failed: %w", err)
		}
	}

	defer func() {
		if err != nil {
			pool.Close()
		}
	}()

	if err = pool.AcquireFunc(ctx, func(c *pgxpool.Conn) error {
		return migrateDB(ctx, c.Conn(), logger)
	}); err != nil {
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	return &Store{db: pool, logger: logger}, nil
}

func (s *Store) Close() error {
	switch x := s.db.(type) {
	case *pgx.Conn:
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		return x.Close(ctx)
	case *pgxpool.Pool:
		x.Close()
		return nil
	case pgx.Tx:
		return nil
	default:
		return fmt.Errorf("close with unknown DB type %T", s.db)
	}
}

func migrateDB(ctx context.Context, conn *pgx.Conn, logger log.Logger) error {
	level.Debug(logger).Log("msg", "NewMigratorEx")

	m, err := migrate.NewMigratorEx(ctx, conn, "public.schema_version", &migrate.MigratorOptions{
		MigratorFS: migrations.FS,
	})
	if err != nil {
		return fmt.Errorf("new migrator: %w", err)
	}

	level.Debug(logger).Log("msg", "LoadMigrations")

	if err = m.LoadMigrations("."); err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}

	level.Debug(logger).Log("msg", "Migrate")

	if err = m.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	level.Debug(logger).Log("msg", "done")

	return nil
}

func (s *Store) Transact(ctx context.Context, f func(store.Store) error) error {
	defer func(begin time.Time) {
		eztrc.Tracef(ctx, "Transact took %s", time.Since(begin))
	}(time.Now())

	retryable := func(err error) bool {
		if pgerr := &(pgconn.PgError{}); errors.As(err, &pgerr) {
			if pgerr.Code == "40001" { // concurrent updates
				return true
			}
		}
		return false
	}

	var err error
	for try, max := 1, 3; try <= max; try++ {
		err = s.transactDirect(ctx, f)
		switch {
		case err == nil:
			return nil
		case retryable(err):
			eztrc.Tracef(ctx, "Transact error (%v), retryable, attempt %d/%d", err, try, max)
		default:
			return err
		}
	}

	return err
}

func (s *Store) transactDirect(ctx context.Context, f func(store.Store) error) error {
	var entered time.Time
	defer func(begin time.Time) {
		if !entered.IsZero() {
			took := entered.Sub(begin)
			metrics.OpWait("pgstore_transactdirect", took)
			eztrc.LazyTracef(ctx, "transactDirect waited for %s", took)
		}
	}(time.Now())

	switch x := s.db.(type) {
	case *pgx.Conn:
		return x.BeginTxFunc(ctx, pgx.TxOptions{
			IsoLevel: pgx.Serializable,
		}, func(tx pgx.Tx) error {
			entered = time.Now()
			return f(&Store{
				db:     tx,
				logger: s.logger,
			})
		})

	case *pgxpool.Pool:
		return x.BeginTxFunc(ctx, pgx.TxOptions{
			IsoLevel: pgx.Serializable,
		}, func(tx pgx.Tx) error {
			entered = time.Now()
			return f(&Store{
				db:     tx,
				logger: s.logger,
			})
		})

	case pgx.Tx:
		return x.BeginFunc(ctx, func(tx pgx.Tx) error {
			entered = time.Now()
			return f(&Store{
				db:     tx,
				logger: s.logger,
			})
		})

	default:
		return fmt.Errorf("unknown DB type %T", s.db)
	}
}

func (s *Store) Ping(ctx context.Context) error {
	var n int
	return s.db.QueryRow(ctx, `select 1`).Scan(&n)
}

const cleanupChallengesQuery = `
delete from challenges
where
  created_at <= now() - '5 minutes'::interval
`

const cleanupAuctionsQuery = `
with
old as (
  select chain_id, height
  from auctions a
  join chains c on (a.chain_id = c.id)
  where
    c.retention_time is not null
    and now() >= (a.created_at + c.retention_time::interval)
),
deleted_bids as (
  delete from bids
  using old
  where
    bids.chain_id = old.chain_id
    and bids.height = old.height
)
delete from auctions
using old
where
  auctions.chain_id = old.chain_id
  and auctions.height = old.height
`

func (s *Store) Cleanup(ctx context.Context) error {
	{
		status, err := s.db.Exec(ctx, cleanupChallengesQuery)
		if err != nil {
			return fmt.Errorf("cleanup challenges: %w", err)
		}

		eztrc.Tracef(ctx, "deleted %d challenges", status.RowsAffected())
	}

	{
		status, err := s.db.Exec(ctx, cleanupAuctionsQuery)
		if err != nil {
			return fmt.Errorf("cleanup auctions: %w", err)
		}

		eztrc.Tracef(ctx, "deleted %d auctions and their bids", status.RowsAffected())
	}

	return nil
}

//
// bids
//

const insertBidQuery = `
insert into bids
(
	id,
	chain_id,
	height,
	kind,
	txs,
	mekatek_payment,
	validator_payment,
	priority,
	state,
	payments
)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
returning
	created_at,
	updated_at
`

func (s *Store) InsertBid(ctx context.Context, b *store.Bid) error {
	if b.ID.IsNil() {
		var err error
		if b.ID, err = uuid.NewV4(); err != nil {
			return fmt.Errorf("uuid gen failed: %w", err)
		}
	}

	return s.db.QueryRow(ctx, insertBidQuery,
		b.ID,
		b.ChainID,
		b.Height,
		b.Kind,
		b.Txs,
		b.MekatekPayment,
		b.ValidatorPayment,
		b.Priority,
		b.State,
		b.Payments,
	).Scan(&b.CreatedAt, &b.UpdatedAt)
}

const updateBidsQuery = `
update bids
set
	state      = updates.state,
	updated_at = now()
from
	jsonb_to_recordset($1)
	as updates(id uuid, state text)
where
	bids.id = updates.id
	and updates.state is not null
	and bids.state <> updates.state
`

func (s *Store) UpdateBids(ctx context.Context, bids ...*store.Bid) error {
	type update struct {
		ID    uuid.UUID      `json:"id"`
		State store.BidState `json:"state,omitempty"`
	}

	updates := make([]update, len(bids))
	for i, b := range bids {
		updates[i] = update{b.ID, b.State}
	}
	if _, err := s.db.Exec(ctx, updateBidsQuery, updates); err != nil {
		return fmt.Errorf("update bids: %w", err)
	}

	return nil
}

const listBidsQuery = `
select
	id,
	chain_id,
	height,
	kind,
	txs,
	mekatek_payment,
	validator_payment,
	priority,
	state,
	payments,
	created_at,
	updated_at
from
	bids
where
	chain_id = $1
	and height = $2
order by
	created_at asc
`

func (s *Store) ListBids(ctx context.Context, chainID string, height int64) ([]*store.Bid, error) {
	rows, err := s.db.Query(ctx, listBidsQuery, chainID, height)
	if err != nil {
		return nil, fmt.Errorf("query rows: %w", err)
	}
	defer rows.Close()

	var bids []*store.Bid
	for rows.Next() {
		var (
			b store.Bid
			// Nullable types below
			mekatekPayment   = &b.MekatekPayment
			validatorPayment = &b.ValidatorPayment
			priority         = &b.Priority
			state            pgtype.Text
			payments         = &b.Payments
		)

		if err = rows.Scan(
			&b.ID,
			&b.ChainID,
			&b.Height,
			&b.Kind,
			&b.Txs,
			&mekatekPayment,
			&validatorPayment,
			&priority,
			&state,
			&payments,
			&b.CreatedAt,
			&b.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		b.State = store.BidState(state.String)

		bids = append(bids, &b)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan err: %w", err)
	}

	return bids, nil
}

//
// auctions
//

const upsertAuctionQuery = `
insert into auctions
(
	chain_id,
	height,
	validator_address,
	validator_allocation,
	validator_payment_address,
	mekatek_payment_address,
	payment_denom,
	finished_at,
	registered_power,
	total_power
)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
on conflict (chain_id, height) do update
set
	finished_at = excluded.finished_at
returning
	created_at
`

func (s *Store) UpsertAuction(ctx context.Context, a *store.Auction) error {
	return s.db.QueryRow(ctx, upsertAuctionQuery,
		a.ChainID,
		a.Height,
		a.ValidatorAddress,
		a.ValidatorAllocation,
		a.ValidatorPaymentAddress,
		a.MekatekPaymentAddress,
		a.PaymentDenom,
		nullTime(a.FinishedAt),
		a.RegisteredPower,
		a.TotalPower,
	).Scan(&a.CreatedAt)
}

const selectAuctionQuery = `
select
	chain_id,
	height,
	validator_address,
	validator_allocation,
	validator_payment_address,
	mekatek_payment_address,
	payment_denom,
	registered_power,
	total_power,
	created_at,
	finished_at
from
	auctions
where
	chain_id = $1 and height = $2
`

func (s *Store) SelectAuction(ctx context.Context, chainID string, height int64) (*store.Auction, error) {
	var a store.Auction
	err := s.db.QueryRow(ctx, selectAuctionQuery, chainID, height).Scan(
		&a.ChainID,
		&a.Height,
		&a.ValidatorAddress,
		&a.ValidatorAllocation,
		&a.ValidatorPaymentAddress,
		&a.MekatekPaymentAddress,
		&a.PaymentDenom,
		&a.RegisteredPower,
		&a.TotalPower,
		&a.CreatedAt,
		&nullable[time.Time]{&a.FinishedAt},
	)
	if err != nil {
		return nil, convertError(err)
	}
	return &a, nil
}

//
// challenges
//

const insertChallengeQuery = `
insert into challenges
(
	id,
	chain_id,
	validator_address,
	pub_key_bytes,
	pub_key_type,
	payment_address,
	challenge
)
values ($1, $2, $3, $4, $5, $6, $7)
returning
	created_at
`

func (s *Store) InsertChallenge(ctx context.Context, c *store.Challenge) error {
	if c.ID.IsNil() {
		id, err := uuid.NewV4()
		if err != nil {
			return fmt.Errorf("generate UUID: %w", err)
		}
		c.ID = id
	}

	return s.db.QueryRow(ctx, insertChallengeQuery,
		c.ID,
		c.ChainID,
		c.ValidatorAddress,
		c.PubKeyBytes,
		c.PubKeyType,
		c.PaymentAddress,
		c.Challenge,
	).Scan(&c.CreatedAt)
}

const selectChallengeQuery = `
select
	id,
	chain_id,
	validator_address,
	pub_key_bytes,
	pub_key_type,
	payment_address,
	challenge,
	created_at
from
	challenges
where
	id = $1
`

func (s *Store) SelectChallenge(ctx context.Context, id string) (*store.Challenge, error) {
	var c store.Challenge
	if err := s.db.QueryRow(ctx, selectChallengeQuery, id).Scan(
		&c.ID,
		&c.ChainID,
		&c.ValidatorAddress,
		&c.PubKeyBytes,
		&c.PubKeyType,
		&c.PaymentAddress,
		&c.Challenge,
		&c.CreatedAt,
	); err != nil {
		return nil, convertError(err)
	}
	return &c, nil
}

const deleteChallengeQuery = `delete from challenges where id = $1`

func (s *Store) DeleteChallenge(ctx context.Context, id string) error {
	result, err := s.db.Exec(ctx, deleteChallengeQuery, id)
	if err != nil {
		return fmt.Errorf("execute delete: %w", err)
	}

	if result.RowsAffected() != 1 {
		return store.ErrNotFound
	}

	return nil
}

//
// validators
//

const upsertValidatorQuery = `
insert into validators
(
	chain_id,
	address,
	moniker,
	pub_key_bytes,
	pub_key_type,
	payment_address
)
values ($1, $2, $3, $4, $5, $6)
on conflict (chain_id, address) do update
set
	moniker         = excluded.moniker,
	payment_address = excluded.payment_address,
	updated_at      = now()
returning
	created_at,
	updated_at
`

func (s *Store) UpsertValidator(ctx context.Context, v *store.Validator) error {
	return s.db.QueryRow(ctx, upsertValidatorQuery,
		v.ChainID,
		v.Address,
		v.Moniker,
		v.PubKeyBytes,
		v.PubKeyType,
		v.PaymentAddress,
	).Scan(&v.CreatedAt, &v.UpdatedAt)
}

const selectValidatorQuery = `
select
	chain_id,
	address,
	coalesce(moniker, ''),
	pub_key_bytes,
	pub_key_type,
	payment_address,
	created_at,
	updated_at
from
	validators
where
	chain_id = $1
	and address = $2
`

func (s *Store) SelectValidator(ctx context.Context, chainID, addr string) (*store.Validator, error) {
	var v store.Validator
	err := s.db.QueryRow(ctx, selectValidatorQuery, chainID, addr).Scan(
		&v.ChainID,
		&v.Address,
		&v.Moniker,
		&v.PubKeyBytes,
		&v.PubKeyType,
		&v.PaymentAddress,
		&v.CreatedAt,
		&v.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	return &v, nil
}

const listValidatorsQuery = `
select
	chain_id,
	address,
	coalesce(moniker, ''),
	pub_key_bytes,
	pub_key_type,
	payment_address,
	created_at,
	updated_at
from
	validators
where
	chain_id = $1
order by
	(chain_id, address) asc
`

func (s *Store) ListValidators(ctx context.Context, chainID string) ([]*store.Validator, error) {
	rows, err := s.db.Query(ctx, listValidatorsQuery, chainID)
	if err != nil {
		return nil, fmt.Errorf("query rows: %w", err)
	}
	defer rows.Close()

	var vs []*store.Validator
	for rows.Next() {
		var v store.Validator
		if err = rows.Scan(
			&v.ChainID,
			&v.Address,
			&v.Moniker,
			&v.PubKeyBytes,
			&v.PubKeyType,
			&v.PaymentAddress,
			&v.CreatedAt,
			&v.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		vs = append(vs, &v)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan err: %w", err)
	}

	return vs, nil
}

//
// chains
//

const upsertChainQuery = `
insert into chains
(
	id,
	network,
	mekatek_payment_address,
	payment_denom,
	timeout,
	node_uris
)
values ($1, $2, $3, $4, $5, $6)
on conflict (id) do update
set
	network                 = excluded.network,
	mekatek_payment_address = excluded.mekatek_payment_address,
	payment_denom           = excluded.payment_denom,
	timeout                 = excluded.timeout,
	node_uris               = excluded.node_uris,
	updated_at              = now()
returning
	created_at,
	updated_at
`

func (s *Store) UpsertChain(ctx context.Context, c *store.Chain) error {
	return s.db.QueryRow(ctx, upsertChainQuery,
		c.ID,
		c.Network,
		c.MekatekPaymentAddress,
		c.PaymentDenom,
		c.Timeout.String(),
		c.NodeURIs,
	).Scan(&c.CreatedAt, &c.UpdatedAt)
}

const selectChainQuery = `
select
	id,
	network,
	mekatek_payment_address,
	payment_denom,
	timeout,
	node_uris,
	created_at,
	updated_at
from
	chains
where
	id = $1
`

func (s *Store) SelectChain(ctx context.Context, id string) (*store.Chain, error) {
	var c store.Chain
	err := s.db.QueryRow(ctx, selectChainQuery, id).Scan(
		&c.ID,
		&c.Network,
		&c.MekatekPaymentAddress,
		&c.PaymentDenom,
		&duration{D: &c.Timeout},
		&c.NodeURIs,
		&c.CreatedAt,
		&c.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	return &c, nil
}

const listChainsQuery = `
select
	id,
	network,
	mekatek_payment_address,
	payment_denom,
	timeout,
	node_uris,
	created_at,
	updated_at
from
	chains
order by
	id asc
`

func (s *Store) ListChains(ctx context.Context) ([]*store.Chain, error) {
	rows, err := s.db.Query(ctx, listChainsQuery)
	if err != nil {
		return nil, fmt.Errorf("query rows: %w", err)
	}
	defer rows.Close()

	var chains []*store.Chain
	for rows.Next() {
		var c store.Chain
		if err = rows.Scan(
			&c.ID,
			&c.Network,
			&c.MekatekPaymentAddress,
			&c.PaymentDenom,
			&duration{D: &c.Timeout},
			&c.NodeURIs,
			&c.CreatedAt,
			&c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		chains = append(chains, &c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan err: %w", err)
	}

	return chains, nil
}

//
//
//

type nullable[T any] struct{ V *T }

// Scan implements the Scanner interface.
func (v *nullable[T]) Scan(value any) error {
	*v.V, _ = value.(T)
	return nil
}

// Value implements the driver Valuer interface.
func (v *nullable[T]) Value() (driver.Value, error) {
	if v.V == nil {
		return nil, nil
	}
	return &v.V, nil
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

type duration struct{ D *time.Duration }

// Scan implements the Scanner interface.
func (v *duration) Scan(value any) error {
	switch t := value.(type) {
	case int64:
		*v.D = time.Duration(t)
	case string:
		d, err := time.ParseDuration(t)
		if err != nil {
			return fmt.Errorf("parse duration: %w", err)
		}
		*v.D = d
	default:
		return fmt.Errorf("can't scan %T into duration", t)
	}

	return nil
}

func convertError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ErrNotFound
	}
	return err
}

//
//
//

type pgDebugLogAdapter struct{ log.Logger }

func (a *pgDebugLogAdapter) Log(ctx context.Context, pgxlevel pgx.LogLevel, msg string, data map[string]interface{}) {
	keyvals := []interface{}{
		"pgxlevel", pgxlevel.String(),
		"msg", msg,
	}
	for k, v := range data {
		keyvals = append(keyvals, k, fmt.Sprintf("%v", v))
	}
	level.Debug(a.Logger).Log(keyvals...)
}
