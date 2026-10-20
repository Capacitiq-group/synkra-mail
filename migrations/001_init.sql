-- Synkra Email — initial schema (design doc v1.2 + owner decisions 2026-10-20)
-- Conventions: schema `mail`, UUIDv7 keys, timestamptz UTC, citext emails, jsonb payloads.

create extension if not exists citext;
create schema if not exists mail;

create table mail.plan (
  id                          uuid primary key,
  slug                        citext not null unique,
  name                        text not null,
  price_cents                 integer,
  currency                    text not null default 'ZAR',
  "interval"                  text not null default 'month',
  mailbox_limit               integer not null,
  alias_limit                 integer not null,
  shared_mailbox_limit        integer not null,
  storage_total_mb            integer not null,
  storage_overage_cents_per_gb integer,
  created_at                  timestamptz not null default now()
);

create table mail.org (
  id                uuid primary key,
  name              text not null,
  plan_id           uuid not null references mail.plan(id),
  status            text not null default 'active',      -- active | suspended
  tier              text not null default 'new',         -- new | verified  (sending tiers, Section 7.3)
  sending_enabled   boolean not null default false,      -- owner decision: no outbound until payment or admin approval
  sending_blocked_reason text,
  review_status     text not null default 'pending',     -- pending | approved | declined  (manual review queue, Section 7.5)
  storage_overage_gb numeric not null default 0,          -- recorded, manually invoiced (owner decision)
  synkra_org_id     uuid,                                 -- optional link only (Section 2.6)
  created_at        timestamptz not null default now(),
  updated_at        timestamptz not null default now()
);

create table mail.subscription (
  id                  uuid primary key,
  org_id              uuid not null references mail.org(id),
  plan_id             uuid not null references mail.plan(id),
  status              text not null default 'trial',     -- trial | active | past_due | canceled
  trial_ends_at       timestamptz,                        -- 14-day trial (owner decision)
  paystack_ref        text,                               -- reserved for the billing document
  current_period_end  timestamptz,
  created_at          timestamptz not null default now(),
  updated_at          timestamptz not null default now()
);
create index subscription_org_idx on mail.subscription (org_id, created_at desc);

create table mail.user (
  id              uuid primary key,
  org_id          uuid not null references mail.org(id),
  email           citext not null,
  password_hash   text,                                  -- argon2id; null = SSO-only (linked orgs, later)
  totp_secret_enc text,                                  -- optional 2FA, encrypted at rest
  display_name    text not null,
  role            text not null default 'member',        -- owner | admin | member
  is_platform_admin boolean not null default false,      -- internal staff (admin portal, Section 11)
  status          text not null default 'active',        -- active | suspended
  synkra_user_id  uuid,                                  -- optional link
  last_login_at   timestamptz,
  created_at      timestamptz not null default now(),
  updated_at      timestamptz not null default now(),
  unique (org_id, email)
);
create index user_email_idx on mail.user (email);

create table mail.session (
  id              uuid primary key,
  user_id         uuid not null references mail.user(id),
  token_hash      text not null unique,                  -- sha256 hex of the bearer token
  kind            text not null default 'web',           -- web | admin | impersonation
  ip              text,
  user_agent      text,
  impersonated_by uuid,                                  -- admin user id when kind='impersonation'
  expires_at      timestamptz not null,
  revoked_at      timestamptz,
  created_at      timestamptz not null default now()
);
create index session_user_idx on mail.session (user_id);

create table mail.app_password (
  id            uuid primary key,
  user_id       uuid not null references mail.user(id),
  name          text not null,
  prefix        text not null,                           -- shown in UI for identification
  password_hash text not null,                          -- argon2id
  expires_at    timestamptz not null,                    -- 180 days (owner decision)
  last_used_at  timestamptz,
  revoked_at    timestamptz,
  created_at    timestamptz not null default now()
);
create index app_password_user_idx on mail.app_password (user_id);

create table mail.domain (
  id               uuid primary key,
  org_id           uuid references mail.org(id),         -- null = platform domain
  name             citext not null unique,
  is_platform      boolean not null default false,
  status           text not null default 'pending',      -- pending | verified | disabled
  ownership_token  text not null,
  dkim_selector    text not null default 'sk1',
  dkim_private_pem text,                                 -- encrypted at rest in prod (Section 7.6); dev: plain
  dkim_public_txt  text,
  verified_at      timestamptz,
  created_at       timestamptz not null default now(),
  updated_at       timestamptz not null default now()
);

create table mail.mailbox (
  id             uuid primary key,
  org_id         uuid not null references mail.org(id),
  owner_user_id  uuid references mail.user(id),          -- null for shared mailboxes
  kind           text not null default 'user',           -- user | shared
  name           text not null,
  send_status    text not null default 'active',         -- active | paused_bounces | paused_abuse | paused_admin
  paused_reason  text,
  created_at     timestamptz not null default now(),
  updated_at     timestamptz not null default now()
);
create index mailbox_org_idx on mail.mailbox (org_id);

create table mail.address (
  id            uuid primary key,
  org_id        uuid not null references mail.org(id),
  domain_id     uuid not null references mail.domain(id),
  mailbox_id    uuid not null references mail.mailbox(id),
  local         citext not null,
  kind          text not null default 'user',            -- user | alias | forward
  forward_target text,                                   -- forwarding aliases (ADR later, Section 4.1)
  is_primary    boolean not null default false,
  allow_send_as boolean not null default true,
  created_at    timestamptz not null default now(),
  unique (domain_id, local)
);
create index address_mailbox_idx on mail.address (mailbox_id);

create table mail.mailbox_member (
  id          uuid primary key,
  mailbox_id  uuid not null references mail.mailbox(id),
  user_id     uuid not null references mail.user(id),
  role        text not null default 'member',            -- owner | member | viewer
  created_at  timestamptz not null default now(),
  unique (mailbox_id, user_id)
);

create table mail.folder (
  id             uuid primary key,
  mailbox_id     uuid not null references mail.mailbox(id),
  name           text not null,
  special_use    text,                                   -- inbox|sent|drafts|trash|junk|archive|null
  uid_validity   bigint not null default 1,
  uid_next       bigint not null default 1,
  total_messages integer not null default 0,
  unread         integer not null default 0,
  subscribed     boolean not null default true,
  created_at     timestamptz not null default now(),
  unique (mailbox_id, name)
);
create index folder_mailbox_idx on mail.folder (mailbox_id);

create table mail.blob (
  sha256      text primary key,
  size_bytes  bigint not null,
  refcount    integer not null default 0,
  stored_at   timestamptz not null default now(),
  purge_after timestamptz
);

create table mail.thread (
  id           uuid primary key,
  org_id       uuid not null,
  subject_norm text,
  assignee_id  uuid references mail.user(id),            -- team-inbox assignment (Section 10.3)
  note         text,                                     -- internal note, never transmitted (Section 10.3)
  created_at   timestamptz not null default now(),
  updated_at   timestamptz not null default now()
);
create index thread_org_idx on mail.thread (org_id, updated_at desc);

create table mail.message (
  id              uuid primary key,
  org_id          uuid not null,
  mailbox_id      uuid not null references mail.mailbox(id),
  blob_sha256     text not null references mail.blob(sha256),
  direction       text not null,                         -- inbound | outbound | internal
  message_id      citext,
  in_reply_to     citext,
  refs            text[],
  thread_id       uuid references mail.thread(id),
  from_email      citext,
  from_name       text,
  to_addrs        jsonb not null default '[]',
  cc_addrs        jsonb not null default '[]',
  subject         text,
  snippet         text not null default '',
  body_text       text,
  has_attachments boolean not null default false,
  size_bytes      bigint not null,
  internal_date   timestamptz not null,
  auth_results    jsonb,
  spam_score      real,
  tracking_enabled boolean not null default false,       -- per-message opt-out (Section 9.1)
  created_at      timestamptz not null default now()
);
create index message_mailbox_idx on mail.message (mailbox_id, internal_date desc);
create index message_thread_idx on mail.message (thread_id);
create index message_message_id_idx on mail.message (message_id);
alter table mail.message add column fts tsvector
  generated always as (
    to_tsvector('simple',
      coalesce(subject,'') || ' ' ||
      coalesce(from_email::text,'') || ' ' ||
      coalesce(from_name,'') || ' ' ||
      coalesce(to_addrs::text,'') || ' ' ||
      coalesce(body_text,''))
  ) stored;
create index message_fts_idx on mail.message using gin (fts);

create table mail.mailbox_item (
  id           uuid primary key,
  folder_id    uuid not null references mail.folder(id),
  message_id   uuid not null references mail.message(id),
  uid          bigint not null,
  flags        text[] not null default '{}',
  deleted_at   timestamptz,                              -- soft delete (Trash semantics live above this)
  created_at   timestamptz not null default now(),
  unique (folder_id, uid)
);
create index mailbox_item_folder_idx on mail.mailbox_item (folder_id, uid desc);
create index mailbox_item_message_idx on mail.mailbox_item (message_id);

create table mail.attachment (
  id           uuid primary key,
  message_id   uuid not null references mail.message(id),
  blob_sha256  text not null references mail.blob(sha256),
  filename     text,
  content_type text not null,
  size_bytes   bigint not null,
  is_inline    boolean not null default false,
  content_id   text
);
create index attachment_message_idx on mail.attachment (message_id);

-- Delivery queue: one job per recipient. Workers claim with FOR UPDATE SKIP LOCKED.
create table mail.delivery_job (
  id                uuid primary key,
  org_id            uuid not null,
  mailbox_id        uuid not null references mail.mailbox(id),
  message_id        uuid not null references mail.message(id),  -- composed RFC822 lives in the sender's blob
  envelope_from     text not null,
  envelope_to       text not null,
  recipient_domain  citext not null,
  is_dsn            boolean not null default false,     -- system-generated bounce/delay notices
  status            text not null default 'queued',     -- queued|in_flight|delivered|deferred|bounced|canceled
  attempts          integer not null default 0,
  next_attempt_at   timestamptz not null default now(),
  lease_until       timestamptz,
  leased_by         text,
  last_code         text,
  last_msg          text,
  scheduled_send_at timestamptz,
  created_at        timestamptz not null default now(),
  updated_at        timestamptz not null default now()
);
create index delivery_due_idx on mail.delivery_job (next_attempt_at)
  where status in ('queued','deferred');
create index delivery_org_idx on mail.delivery_job (org_id, created_at desc);
create index delivery_mailbox_idx on mail.delivery_job (mailbox_id, created_at desc);

create table mail.dsn (
  id              uuid primary key,
  delivery_job_id uuid not null references mail.delivery_job(id),
  kind            text not null,                         -- bounce | delay | success
  is_hard         boolean,
  smtp_code       text,
  reason          text,
  created_at      timestamptz not null default now()
);
create index dsn_job_idx on mail.dsn (delivery_job_id, created_at desc);

create table mail.suppression (
  org_id     uuid not null,
  email      citext not null,
  reason     text not null,                              -- hard_bounce | complaint | unsub
  created_at timestamptz not null default now(),
  primary key (org_id, email)
);

create table mail.tracking_pixel (
  id          uuid primary key,
  message_id  uuid not null references mail.message(id),
  recipient   citext not null,
  created_at  timestamptz not null default now()
);
create index tracking_pixel_message_idx on mail.tracking_pixel (message_id);

create table mail.open_event (
  id            uuid primary key,
  pixel_id      uuid not null references mail.tracking_pixel(id),
  occurred_at   timestamptz not null,
  confidence    text not null,                           -- opened | likely_opened | possibly_automated (Section 9.2)
  client_family text,
  ip_hash       text
);
create index open_event_pixel_idx on mail.open_event (pixel_id, occurred_at);

create table mail.tracked_link (
  id         uuid primary key,
  message_id uuid not null references mail.message(id),
  url        text not null,
  created_at timestamptz not null default now()
);
create index tracked_link_message_idx on mail.tracked_link (message_id);

create table mail.click_event (
  id            uuid primary key,
  link_id       uuid not null references mail.tracked_link(id),
  occurred_at   timestamptz not null,
  client_family text,
  ip_hash       text
);
create index click_event_link_idx on mail.click_event (link_id, occurred_at);

create table mail.quarantine (
  id         uuid primary key,
  org_id     uuid not null,
  mailbox_id uuid,
  message_id uuid,
  reason     text not null,                              -- spam | malware | policy | dmarc
  score      real,
  expires_at timestamptz not null,
  created_at timestamptz not null default now()
);
create index quarantine_org_idx on mail.quarantine (org_id, created_at desc);

create table mail.quota_event (
  id         uuid primary key,
  org_id     uuid not null,
  level      integer not null,                           -- 80 | 90 | 100 | 110 (percent of included allowance)
  note       text,
  created_at timestamptz not null default now(),
  unique (org_id, level)
);

create table mail.audit_event (
  id           uuid primary key,
  actor_user_id uuid,                                    -- acting user (customer or admin)
  admin_user_id uuid,                                    -- staff actor when impersonating
  impersonated boolean not null default false,
  action       text not null,
  target_type  text,
  target_id    text,
  reason       text,
  meta         jsonb,
  created_at   timestamptz not null default now()
);
create index audit_event_created_idx on mail.audit_event (created_at desc);
create index audit_event_target_idx on mail.audit_event (target_type, target_id);

create table mail.rate_event (
  id         uuid primary key,
  org_id     uuid not null,
  mailbox_id uuid,
  kind       text not null,                              -- daily_org | hourly_mailbox | recipients | bounce_breaker
  detail     jsonb,
  created_at timestamptz not null default now()
);
create index rate_event_org_idx on mail.rate_event (org_id, created_at desc);

create table mail.storage_ledger (
  id         uuid primary key,
  org_id     uuid not null,
  month      date not null,                              -- billing month (first day)
  overage_gb integer not null default 0,
  note       text,
  created_at timestamptz not null default now(),
  unique (org_id, month)
);

create table mail.login_attempt (
  id         bigserial primary key,
  email      text,
  ip         text,
  ok         boolean not null,
  created_at timestamptz not null default now()
);
create index login_attempt_key_idx on mail.login_attempt (created_at);

-- Folder counters are maintained by triggers (single source of truth for IMAP STATUS).
create or replace function mail.folder_counter_adj() returns trigger as $$
declare f_new uuid; f_old uuid; d_seen int; d_total int;
begin
  if tg_op = 'INSERT' then
    d_total := 1;
    if '\Seen' = any(new.flags) then d_seen := 0; else d_seen := 1; end if;
    update mail.folder set total_messages = total_messages + 1, unread = unread + d_seen where id = new.folder_id;
    return new;
  elsif tg_op = 'DELETE' then
    d_total := -1;
    if '\Seen' = any(old.flags) then d_seen := 0; else d_seen := -1; end if;
    update mail.folder set total_messages = total_messages - 1, unread = unread + d_seen where id = old.folder_id;
    return old;
  elsif tg_op = 'UPDATE' then
    f_old := old.folder_id; f_new := new.folder_id;
    if old.deleted_at is null and new.deleted_at is not null then
      -- soft delete: leaves folder counters
      update mail.folder set total_messages = total_messages - 1,
        unread = unread - (case when '\Seen' = any(old.flags) then 0 else 1 end)
      where id = f_old;
      return new;
    elsif old.deleted_at is not null and new.deleted_at is null then
      update mail.folder set total_messages = total_messages + 1,
        unread = unread + (case when '\Seen' = any(new.flags) then 0 else 1 end)
      where id = f_new;
      return new;
    else
      if f_old <> f_new then
        update mail.folder set total_messages = total_messages - 1,
          unread = unread - (case when '\Seen' = any(old.flags) then 0 else 1 end)
        where id = f_old;
        update mail.folder set total_messages = total_messages + 1,
          unread = unread + (case when '\Seen' = any(new.flags) then 0 else 1 end)
        where id = f_new;
        return new;
      else
        if ('\Seen' = any(old.flags)) <> ('\Seen' = any(new.flags)) then
          update mail.folder set unread = unread + (case when '\Seen' = any(new.flags) then -1 else 1 end)
          where id = f_new;
        end if;
        return new;
      end if;
    end if;
  end if;
  return null;
end;
$$ language plpgsql;

create trigger mailbox_item_counters after insert or update or delete on mail.mailbox_item
  for each row execute function mail.folder_counter_adj();

-- Ingest events fan out to IMAP IDLE watchers via Postgres NOTIFY.
create or replace function mail.notify_item_change() returns trigger as $$
declare payload text;
begin
  if tg_op = 'INSERT' then
    payload := (select mailbox_id from mail.folder where id = new.folder_id) || ':' || 'append:' || new.folder_id || ':' || new.uid;
    perform pg_notify('mail_events', payload);
  elsif tg_op = 'UPDATE' and old.folder_id = new.folder_id then
    payload := (select mailbox_id from mail.folder where id = new.folder_id) || ':' || 'flags:' || new.folder_id || ':' || new.uid;
    perform pg_notify('mail_events', payload);
  elsif tg_op = 'UPDATE' then
    payload := (select mailbox_id from mail.folder where id = old.folder_id) || ':' || 'expunge:' || old.folder_id || ':' || old.uid;
    perform pg_notify('mail_events', payload);
    payload := (select mailbox_id from mail.folder where id = new.folder_id) || ':' || 'append:' || new.folder_id || ':' || new.uid;
    perform pg_notify('mail_events', payload);
  elsif tg_op = 'DELETE' then
    payload := (select mailbox_id from mail.folder where id = old.folder_id) || ':' || 'expunge:' || old.folder_id || ':' || old.uid;
    perform pg_notify('mail_events', payload);
  end if;
  return null;
end;
$$ language plpgsql;

create trigger mailbox_item_notify after insert or update or delete on mail.mailbox_item
  for each row execute function mail.notify_item_change();

-- Seed the Business plan (Section 10.7).
insert into mail.plan (id, slug, name, price_cents, currency, "interval",
                       mailbox_limit, alias_limit, shared_mailbox_limit,
                       storage_total_mb, storage_overage_cents_per_gb)
values ('01900000-0000-7000-8000-000000000001', 'business', 'Business', 6900, 'ZAR', 'month',
        10, 10, 1, 10240, 1900)
on conflict (slug) do nothing;
