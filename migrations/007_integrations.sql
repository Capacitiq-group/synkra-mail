-- Build register §13 (P3 differentiators): outbound webhooks and API keys —
-- the "the API is the product" surfaces.

create table mail.webhook (
  id         uuid primary key,
  org_id     uuid not null references mail.org(id),
  url        text not null,
  secret     text not null,
  events     text[] not null default '{delivered,bounced,deferred}',
  active     boolean not null default true,
  created_at timestamptz not null default now()
);

-- Fan-out queue: the worker POSTs each row with an HMAC signature and retries.
create table mail.webhook_delivery (
  id         uuid primary key,
  webhook_id uuid not null references mail.webhook(id),
  event_id   uuid not null references mail.delivery_event(id),
  payload    jsonb not null,
  status     text not null default 'queued',  -- queued | delivered | failed
  attempts   integer not null default 0,
  last_error text,
  next_at    timestamptz not null default now(),
  created_at timestamptz not null default now()
);
create index webhook_delivery_due_idx on mail.webhook_delivery (status, next_at);

create table mail.api_key (
  id           uuid primary key,
  org_id       uuid not null references mail.org(id),
  created_by   uuid not null references mail.user(id),
  name         text not null,
  token_hash   text not null unique,             -- sha256 hex of the full token
  prefix       text not null,                    -- sk_<8hex> shown in listings
  last_used_at timestamptz,
  expires_at   timestamptz,
  revoked_at   timestamptz,
  created_at   timestamptz not null default now()
);
