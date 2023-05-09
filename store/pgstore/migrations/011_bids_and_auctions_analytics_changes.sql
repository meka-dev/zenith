alter table auctions add column registered_power  bigint;
alter table auctions add column total_power       bigint;
alter table bids     add column mekatek_payment   bigint;
alter table bids     add column validator_payment bigint;
alter table bids     add column priority          bigint;
alter table bids     add column state             text  ;
alter table bids     add column payments          jsonb ;
