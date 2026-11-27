-- 008_audit_hardening.sql — the 27 November 2026 external audit fixes.
-- Each block names the audit item it closes.

-- (13) login e-mail is globally unique: login resolves by e-mail alone, so a
-- shared address across orgs bound to an arbitrary row.
create unique index if not exists user_email_global_idx on mail.user (email);

-- (13) e-mail verification: flag + one-time token table (endpoints land with it).
alter table mail.user add column if not exists email_verified_at timestamptz;
create table if not exists mail.verification_token (
  id         uuid primary key,
  user_id    uuid not null references mail.user(id),
  token_hash text not null unique,
  kind       text not null default 'email',          -- email | password_reset
  expires_at timestamptz not null,
  used_at    timestamptz,
  created_at timestamptz not null default now()
);
create index if not exists verification_user_idx on mail.verification_token (user_id);

-- (3) MFA: enrolment flag next to the sealed seed (totp_secret_enc existed; the flag did not).
alter table mail.user add column if not exists totp_enabled boolean not null default false;

-- (6) the throttle query filters by email and by ip; created_at alone cannot serve it,
-- so a login storm degraded into a full-table scan.
create index if not exists login_attempt_email_idx on mail.login_attempt (email, created_at);
create index if not exists login_attempt_ip_idx on mail.login_attempt (ip, created_at);

-- (23) the quota check sums size_bytes over org_id on every inbound and outbound
-- message; without this index every delivery scanned the whole table.
create index if not exists message_org_idx on mail.message (org_id);

-- (27) trigram support for substring search (ILIKE with wildcards is unindexable otherwise).
create extension if not exists pg_trgm;
create index if not exists message_subject_trgm_idx on mail.message using gin (subject gin_trgm_ops);

-- (26) a generated tsvector column cannot hold more than 1 MB of lexemes, so a
-- large plain-text body failed the INSERT forever (451 on every delivery retry).
-- Truncate the expression: oversized bodies index a prefix instead of failing.
drop index if exists mail.message_fts_idx;
alter table mail.message drop column if exists fts;
alter table mail.message add column fts tsvector generated always as (
  to_tsvector('simple',
    left(coalesce(subject,''), 10000) || ' ' ||
    left(coalesce(from_email::text,''), 200) || ' ' ||
    left(coalesce(from_name,''), 200) || ' ' ||
    left(coalesce(to_addrs::text,''), 10000) || ' ' ||
    left(coalesce(body_text,''), 400000))
) stored;
create index message_fts_idx on mail.message using gin (fts);

-- (28) the claim query includes in_flight jobs; the partial index did not cover
-- them, so every 2-second poll degenerated toward a full scan.
drop index if exists mail.delivery_due_idx;
create index delivery_due_idx on mail.delivery_job (next_attempt_at)
  where status in ('queued','deferred','in_flight');

-- (29) UIDVALIDITY was pinned at 1: a deleted-and-recreated folder kept the same
-- UID space and IMAP clients showed stale cache entries. New folders derive it
-- from creation time (existing rows keep their value; a rebuild also re-keys).
alter table mail.folder alter column uid_validity set default (extract(epoch from now())::bigint);

-- (20) atomic send-limit counters: one statement reserves capacity, so two
-- concurrent submissions cannot both read 99/100 used and both send.
create table if not exists mail.send_counter (
  counter_key text primary key,                       -- org:<uuid>:<yyyymmdd> | mb:<uuid>:<yyyymmddhh>
  used        integer not null default 0,
  updated_at  timestamptz not null default now()
);

-- (4) staged DKIM rotation bookkeeping; the private material is stored sealed
-- ("enc1." prefix) from this build on (Section 7.6).
alter table mail.domain add column if not exists dkim_next_selector text;
alter table mail.domain add column if not exists dkim_next_private_enc text;
alter table mail.domain add column if not exists dkim_next_public_txt text;
alter table mail.domain add column if not exists reverify_due_at timestamptz;

-- (11) unverified-domain squatting: registrations that never verify expire and
-- the janitor releases them (re-verification is flagged, alerts first).
alter table mail.domain add column if not exists expires_at timestamptz;
-- The global unique(name) constraint would keep a squatted name locked forever
-- even after expiry, so uniqueness applies to live rows only.
alter table mail.domain drop constraint if exists domain_name_key;
create unique index if not exists domain_name_live_idx on mail.domain (name) where status <> 'expired';

-- Phase 8 (register §4b): the zone store behind DnsService. Zone data is
-- customer personal information (POPIA): the audit log records every change.
create table if not exists mail.dns_zone (
  id           uuid primary key,
  zone         citext not null unique,                -- the zone origin, e.g. example.co.za
  kind         text not null default 'customer',      -- platform | customer | reverse
  owner_org_id uuid references mail.org(id),          -- null for the platform zone
  serial       bigint not null default 1,
  created_at   timestamptz not null default now(),
  updated_at   timestamptz not null default now()
);
create table if not exists mail.dns_record (
  id         uuid primary key,
  zone_id    uuid not null references mail.dns_zone(id) on delete cascade,
  name       text not null,                          -- relative to the zone origin
  rrtype     text not null,                          -- MX | TXT | A | AAAA | CNAME | NS | SRV | CAA
  rdata      text not null,
  ttl        integer not null default 300,           -- 4b.9: onboarding-speed TTL policy
  purpose    text not null default '',               -- mail_mx | spf | dkim | dmarc | ownership | mta_sts | tls_rpt | custom
  created_at timestamptz not null default now(),
  unique (zone_id, name, rrtype, rdata)
);
create index if not exists dns_record_zone_idx on mail.dns_record (zone_id, name);
create table if not exists mail.dns_audit (
  id         bigserial primary key,
  zone_id    uuid not null references mail.dns_zone(id),
  actor_id   uuid,
  action     text not null,                          -- create_zone | put_record | delete_record | batch_apply
  detail     jsonb not null default '{}',
  created_at timestamptz not null default now()
);
