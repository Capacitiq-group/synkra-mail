-- Build register §7.3 (decision 17.4): the delivery event log — the per-message
-- state machine every trace/dashboard/webhook reads. States:
--   queued → response (delivered | bounced | deferred). "submitted" and
--   "authenticated" are folded into "queued" for v1: admission and auth happen
--   in the same transaction that creates the jobs, and a queued event can only
--   exist for an authenticated, admitted message.
create table mail.delivery_event (
  id          uuid primary key,
  message_id  uuid not null,
  org_id      uuid not null,
  mailbox_id  uuid,                                -- sending mailbox when known
  state       text not null,                       -- queued | delivered | bounced | deferred
  detail      jsonb,
  occurred_at timestamptz not null default now()
);
create index delivery_event_msg_idx on mail.delivery_event (message_id, occurred_at);
create index delivery_event_org_idx on mail.delivery_event (org_id, occurred_at desc);
