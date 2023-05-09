alter table auctions add constraint auctions_chain_id_not_empty check (chain_id != '');
alter table auctions add constraint auctions_validator_address_not_empty check (validator_address != '');
alter table auctions add constraint auctions_validator_payment_address_not_empty check (validator_payment_address != '');
alter table auctions add constraint auctions_mekatek_payment_address_not_empty check (mekatek_payment_address != '');
alter table auctions add constraint auctions_payment_denom_not_empty check (payment_denom != '');

alter table chains add constraint chains_id_not_empty check (id != '');
alter table chains add constraint chains_network_not_empty check (network != '');
alter table chains add constraint chains_mekatek_payment_address_not_empty check (mekatek_payment_address != '');

alter table validators add constraint validators_chain_id_not_empty check (chain_id != '');
alter table validators add constraint validators_address_not_empty check (address != '');
alter table validators add constraint validators_pub_key_bytes_not_empty check (length(pub_key_bytes) != 0);
alter table validators add constraint validators_pub_key_type_not_empty check (pub_key_type != '');
alter table validators add constraint validators_payment_address_not_empty check (payment_address != '');

alter table bids add constraint bids_chain_id_not_empty check (chain_id != '');
alter table bids add constraint bids_height_not_zero check (height != 0);
alter table bids add constraint bids_kind_not_empty check (kind != '');
alter table bids add constraint bids_txs_not_empty check (array_length(txs, 1) != 0);
