# Migrations

Migrations define schema changes and SQL only data migrations in numbered individual SQL files.

## Development

In development, you can create and migrate a local Postgres database for local exploratory testing with the following:

```shell
brew install postgresql
createdb -E UTF8 zenith-dev
go install github.com/jackc/tern@latest
tern migrate --database zenith-dev --conn-string "postgres://${USER}@localhost:5432"
```

Tests that instantiate a `PGStore` create and migrate their own databases, so you don't have to do the above for that purpose.

## Production

For the time being, migrations are ran manually in production with the following command.

```shell
../../infra/secrets/bw-update-secrets
source ../../infra/secrets/blockbuilder_env
go install github.com/jackc/tern@latest
tern migrate \
    --ssh-host fra-api-1 \
    --ssh-user "$USER" \
    --conn-string "$BLOCK_BUILDER_STORE_CONN_STR"
```

## Documentation

[Documentation](docs/README.md) of the fully migrated schema is generated with `go generate ./...`.
