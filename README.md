# Zenith

Mekatek's block builder API lives here for posterity.

## Operations

### Database

```shell
# connect
hack/psql-prod

# show tables
\dt;

# describe a table
\d+ chains;

# update a text[]
BEGIN;
UPDATE chains
 SET node_uris = '{http://hostname:26657}'
 WHERE id = 'whatever-chain-id';
COMMIT;

# reset a localnet
BEGIN;
DELETE FROM bids     WHERE chain_id = 'localnet-chain-id';
DELETE FROM auctions WHERE chain_id = 'localnet-chain-id';
COMMIT;
```

### Releases

To release a new tag of our Tendermint fork you check out the tracking branch
with the patches, and give it a tag of the format `mekatek/REPO@VERSION-PATCH`
where REPO is the upstream e.g. `github.com/tendermint/tendermint`, VERSION is
the upstream version tag that our branch is tracking e.g. `v0.34.19`, and PATCH
is an integer representing Mekatek's changes on that VERSION e.g. `3`.

```shell
cd meka-dev/tendermint
git checkout v0.34.19-mekatek
git tag mekatek/github.com/tendermint/tendermint@v0.34.19-3
git push --tags origin
```

To produce a patched binary, see the build-NETWORK scripts in [hack](hack).

### tmkms

[meka-dev/tmkms](https://github.com/meka-dev/tmkms/tree/v0.34.19-mekatek/) is a
fork of [iqlusion/tmkms](https://github.com/iqlusion/tmkms) that supports the
builder API for validators that use a sentry architecture.

```shell
# build
git clone https://github.com/meka-dev/tmkms --branch v0.34.19-mekatek
cd tmkms
cargo build --release --features=softsign
ls -ltar target/release/tmkms
```

A localnet full node can be configured to use tmkms for signing.
Initialize a tmkms instance on the localnet host.

```shell
tmkms init --networks osmosis /var/meka/tmkms

tmkms softsign import \
    /path/to/config/priv_validator_key.json \
    /var/meka/tmkms/secrets/priv_validator_key
```

Write this config file to `/var/meka/tmkms/tmkms.toml`. Here the key prefixes
assume Osmosis, they'll be different for other networks.

```toml
[[chain]]
id = "localnet-chain-id"
key_format = { type = "bech32", account_key_prefix = "osmopub", consensus_key_prefix = "osmovalconspub" }
state_file = "/var/meka/tmkms/state/localosmosis-consensus.json"

[[providers.softsign]]
chain_ids = ["localosmosis"]
key_type = "consensus"
path = "/var/meka/tmkms/secrets/priv_validator_key"

[[validator]]
chain_id = "localnet-chain-id"
addr = "tcp://localnet-full-node:26659" # <-- CHANGE THE HOST HERE AS NECESSARY
secret_key = "/var/meka/tmkms/secrets/kms-identity.key"
protocol_version = "v0.34"
reconnect = true
```

Modify the localnet full node's config.toml like this.

```toml
# priv_validator_key_file = "config/priv_validator_key.json"   # <-- COMMENT THIS OUT
# priv_validator_state_file = "data/priv_validator_state.json" # <-- COMMENT THIS OUT
priv_validator_laddr = "tcp://0.0.0.0:26659"                   # <-- SET THIS
```

Start tmkms like this.

```shell
tmkms start --config=/var/meka/tmkms/tmkms.toml
```

Restart the localnet full node. It should bind to priv_validator_laddr, accept a
connection from tmkms, and then use that connection whenever it needs to sign
anything.
