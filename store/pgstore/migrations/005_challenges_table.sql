create table challenges
(
    id                uuid        not null primary key,
    chain_id          text        not null references chains (id),
    validator_address text        not null,
    pub_key_bytes     bytea       not null,
    pub_key_type      text        not null,
    payment_address   text        not null,
    allocation        numeric     not null,
    challenge         bytea       not null,
    created_at        timestamptz not null default now()
);

alter table challenges add constraint challenges_chain_id_not_empty check (chain_id != '');
alter table challenges add constraint challenges_validator_address_not_empty check (validator_address != '');
alter table challenges add constraint challenges_pub_key_bytes_not_empty check (length(pub_key_bytes) != 0);
alter table challenges add constraint challenges_pub_key_type_not_empty check (pub_key_type != '');
alter table challenges add constraint challenges_payment_address_not_empty check (payment_address != '');
alter table challenges add constraint challenges_challenge_not_empty check (length(challenge) != 0);

alter table validators drop column challenge;
alter table validators drop column verified_at;
alter table validators drop column deleted_at;
