alter table auctions add column validator_allocation numeric not null;
alter table auctions add column validator_payment_address text not null;
alter table auctions add column mekatek_payment_address text not null;
