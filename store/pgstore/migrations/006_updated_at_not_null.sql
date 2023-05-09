update chains set updated_at = created_at where updated_at is null;
alter table chains alter column updated_at set not null;
alter table chains alter column updated_at set default now();

update validators set updated_at = created_at where updated_at is null;
alter table validators alter column updated_at set not null;
alter table validators alter column updated_at set default now();

update bids set updated_at = created_at where updated_at is null;
alter table bids alter column updated_at set not null;
alter table bids alter column updated_at set default now();
