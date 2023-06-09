module zenith

go 1.19

replace (
	github.com/gogo/protobuf => github.com/regen-network/protobuf v1.3.3-alpha.regen.1
	mekapi => ../mekapi
)

require (
	github.com/NYTimes/gziphandler v1.1.1
	github.com/cosmos/cosmos-sdk v0.46.7
	github.com/go-kit/log v0.2.1
	github.com/gofrs/uuid v4.3.0+incompatible
	github.com/google/go-cmp v0.5.9
	github.com/gorilla/mux v1.8.0
	github.com/hashicorp/go-multierror v1.1.1
	github.com/jackc/pgconn v1.12.1
	github.com/jackc/pgtype v1.11.0
	github.com/jackc/pgx/v4 v4.16.1
	github.com/jackc/tern v1.13.0
	github.com/meka-dev/mekatek-go v0.0.14
	github.com/peterbourgon/ff/v3 v3.3.0
	github.com/prometheus/client_golang v1.13.0
	github.com/tendermint/tendermint v0.34.24
	golang.org/x/exp v0.0.0-20220921164117-439092de6870
	golang.org/x/sync v0.1.0
	mekapi v0.0.0-00010101000000-000000000000
)

require (
	github.com/Masterminds/goutils v1.1.0 // indirect
	github.com/Masterminds/semver v1.5.0 // indirect
	github.com/Masterminds/sprig v2.22.0+incompatible // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/btcsuite/btcd v0.22.1 // indirect
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/cosmos/btcutil v1.0.5 // indirect
	github.com/go-logfmt/logfmt v0.5.1 // indirect
	github.com/go-stack/stack v1.8.1 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/huandu/xstrings v1.3.2 // indirect
	github.com/imdario/mergo v0.3.13 // indirect
	github.com/jackc/chunkreader/v2 v2.0.1 // indirect
	github.com/jackc/pgio v1.0.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgproto3/v2 v2.3.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20200714003250-2b9c44734f2b // indirect
	github.com/jackc/puddle v1.2.1 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.2-0.20181231171920-c182affec369 // indirect
	github.com/mitchellh/copystructure v1.0.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.0 // indirect
	github.com/oklog/ulid/v2 v2.1.0 // indirect
	github.com/petermattis/goid v0.0.0-20180202154549-b0b1615b78e5 // indirect
	github.com/prometheus/client_model v0.2.0 // indirect
	github.com/prometheus/common v0.37.0 // indirect
	github.com/prometheus/procfs v0.8.0 // indirect
	github.com/sasha-s/go-deadlock v0.3.1 // indirect
	golang.org/x/crypto v0.3.0 // indirect
	golang.org/x/sys v0.3.0 // indirect
	golang.org/x/text v0.5.0 // indirect
	google.golang.org/protobuf v1.28.2-0.20220831092852-f930b1dc76e8 // indirect
)
