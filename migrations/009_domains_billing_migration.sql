-- 009_domains_billing_migration.sql — phase 11 (28 November 2026):
--   (a) registrar-managed domain lifecycle (Oner reseller, ZARC states),
--   (b) the billing document (invoices with per-line VAT, payment methods,
--       payment history, org billing contact),
--   (c) the IMAP migration assistant (jobs + per-message items).

-- ===== (a) Registrar-managed domains =====
-- lifecycle_state mirrors the registry's grace windows for .ZA TLDs:
--   add_grace    — first days after registration, delete-for-credit window
--   ok           — in force
--   renew_grace  — just renewed
--   redemption   — expired, paid restore window
--   pending_delete — queued for drop
--   expired      — lapsed, not yet in redemption
alter table mail.domain add column if not exists registrar text not null default 'manual';
-- manual | oner
alter table mail.domain add column if not exists registrar_domain_id text;
alter table mail.domain add column if not exists registry_status text not null default 'ok';
-- ok | pending_create | pending_update | client_hold | server_hold
alter table mail.domain add column if not exists lifecycle_state text not null default 'ok';
alter table mail.domain add column if not exists registered_at timestamptz;
alter table mail.domain add column if not exists renews_until timestamptz;
alter table mail.domain add column if not exists auto_renew boolean not null default false;
alter table mail.domain add column if not exists transfer_lock boolean not null default false;
alter table mail.domain add column if not exists whois_privacy boolean not null default false;
-- EPP/auth-code retrieval is sensitive: record when it was last issued (step-up audit pairs).
alter table mail.domain add column if not exists epp_code_last_at timestamptz;
-- Registrant contact as published to the registry (WHOIS record).
alter table mail.domain add column if not exists registrant jsonb;
-- Current delegation. 'full' = we run the zone on our authoritative DNS;
-- 'partial' = customer keeps their own nameservers (mail records still ours).
alter table mail.domain add column if not exists nameservers text[] not null default '{}';
alter table mail.domain add column if not exists managed_mode text not null default 'unmanaged';
-- unmanaged | full | partial
alter table mail.domain add column if not exists last_status_sync_at timestamptz;

-- ===== (b) Billing =====
alter table mail.org add column if not exists billing_email text;
alter table mail.org add column if not exists billing_contact_name text;
alter table mail.org add column if not exists vat_number text;

-- Lines are jsonb: [{description, quantity, unit_zar, vat_zar, total_zar}] —
-- VAT is stored PER LINE at 15% and re-checked in code before display.
create table if not exists mail.invoice (
  id            uuid primary key,
  org_id        uuid not null references mail.org(id),
  number        text not null unique,               -- SM-2026-0001
  status        text not null default 'open',       -- open | paid | void
  currency      text not null default 'ZAR',
  subtotal_zar  numeric(12,2) not null default 0,
  vat_zar       numeric(12,2) not null default 0,
  total_zar     numeric(12,2) not null default 0,
  issued_at     timestamptz not null default now(),
  due_at        timestamptz,
  paid_at       timestamptz,
  provider      text not null default 'paystack',
  provider_ref  text,                               -- Paystack transaction ref
  pay_url       text,                               -- Paystack authorization_url
  lines         jsonb not null default '[]',
  created_at    timestamptz not null default now()
);
create index if not exists invoice_org_idx on mail.invoice (org_id, issued_at desc);

create table if not exists mail.payment_method (
  id          uuid primary key,
  org_id      uuid not null references mail.org(id),
  provider    text not null default 'paystack',
  kind        text not null default 'card',         -- card | bank_transfer | eft
  masked      text not null,                        -- e.g. "•••• 4081 (Visa)"
  holder      text,
  exp_month   int,
  exp_year    int,
  is_default  boolean not null default false,
  created_at  timestamptz not null default now()
);

create table if not exists mail.payment (
  id           uuid primary key,
  org_id       uuid not null references mail.org(id),
  invoice_id   uuid references mail.invoice(id),
  amount_zar   numeric(12,2) not null,
  currency     text not null default 'ZAR',
  status       text not null default 'pending',    -- pending | success | failed | refunded
  provider     text not null default 'paystack',
  provider_ref text unique,
  paid_at      timestamptz,
  created_at   timestamptz not null default now()
);
create index if not exists payment_org_idx on mail.payment (org_id, created_at desc);

-- ===== (c) IMAP migration assistant =====
-- source: {host, port, security: 'none'|'starttls'|'tls', username, password_sealed}
create table if not exists mail.migration_job (
  id             uuid primary key,
  org_id         uuid not null references mail.org(id),
  mailbox_id     uuid not null references mail.mailbox(id),
  created_by     uuid not null references mail.user(id),
  source         jsonb not null,
  state          text not null default 'queued',
  -- queued | running | paused | completed | failed | cancelled
  total_messages int not null default 0,
  migrated       int not null default 0,
  failed         int not null default 0,
  folder_map     jsonb not null default '{}',       -- {sourceFolder: destFolderName}
  last_error     text,
  heartbeat_at   timestamptz,
  started_at     timestamptz,
  completed_at   timestamptz,
  created_at     timestamptz not null default now(),
  updated_at     timestamptz not null default now()
);
create index if not exists migration_job_org_idx on mail.migration_job (org_id, created_at desc);

create table if not exists mail.migration_item (
  id         uuid primary key,
  job_id     uuid not null references mail.migration_job(id) on delete cascade,
  folder     text not null,
  uid        bigint not null,
  message_id text,
  subject    text,
  from_addr  text,
  size_bytes bigint not null default 0,
  state      text not null default 'pending',
  -- pending | migrated | failed | skipped
  attempts   int not null default 0,
  error      text,
  updated_at timestamptz not null default now()
);
create index if not exists migration_item_state_idx on mail.migration_item (job_id, state);
