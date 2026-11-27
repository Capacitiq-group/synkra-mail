-- Phase 1 of the consolidated build register (§1 API and authentication hardening).

-- 1.9 Idempotency keys: the first response to a keyed request is stored per
-- user + key and replayed on retry; a key reused with a different body is a 422.
create table mail.idempotency (
  id           uuid primary key,
  user_id      uuid not null references mail.user(id),
  key          text not null,
  endpoint     text not null,                            -- "send" | "addresses" | "domains" | "domains/verify"
  request_hash text not null,                            -- sha256 of the raw request body
  status       integer not null,
  response     jsonb,
  created_at   timestamptz not null default now(),
  unique (user_id, key)
);
create index idempotency_created_idx on mail.idempotency (created_at);
