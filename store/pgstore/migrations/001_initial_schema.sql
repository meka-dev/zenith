create table chains
(
    id                      text        not null unique primary key,
    network                 text        not null,
    mekatek_payment_address text        not null,
    created_at              timestamptz not null default now(),
    updated_at              timestamptz
);

create table validators
(
    chain_id        text        not null references chains (id),
    address         text        not null unique,
    pub_key_bytes   bytea       not null,
    pub_key_type    text        not null,
    payment_address text        not null,
    allocation      numeric     not null,
    challenge       bytea,
    created_at      timestamptz not null default now(),
    verified_at     timestamptz,
    updated_at      timestamptz,
    deleted_at      timestamptz,

    primary key (chain_id, address)
);

create index validators_deleted_at_idx on validators (deleted_at);

create table auctions
(
    chain_id          text        not null references chains (id),
    height            bigint      not null,
    validator_address text        not null references validators (address),
    created_at        timestamptz not null default now(),
    finished_at       timestamptz,

    primary key (chain_id, height)
);

create index auctions_finished_at_idx on auctions (finished_at);

create extension if not exists "uuid-ossp";

create table bids
(
    id         uuid        primary key,
    chain_id   text        not null,
    height     bigint      not null,
    kind       text        not null,
    txs        bytea[]     not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz,

    foreign key (chain_id, height) references auctions (chain_id, height)
);