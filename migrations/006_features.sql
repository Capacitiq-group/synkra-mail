-- Build register §12 (P2 feature register): the account-feature surfaces the
-- endpoints in handlers_features.go serve.

-- 12b signatures (webmail settings).
create table mail.signature (
  id         uuid primary key,
  user_id    uuid not null references mail.user(id),
  name       text not null,
  html       text not null default '',
  text       text not null default '',
  is_default boolean not null default false,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (user_id, name)
);

-- 12c autoresponder (one per mailbox; window-scoped; one reply per sender per day).
create table mail.autoresponder (
  id         uuid primary key,
  mailbox_id uuid not null unique references mail.mailbox(id),
  enabled    boolean not null default false,
  subject    text not null,
  body       text not null,
  starts_at  timestamptz,
  ends_at    timestamptz,
  updated_at timestamptz not null default now()
);
create table mail.autoresponder_log (
  mailbox_id uuid not null references mail.mailbox(id),
  sender     citext not null,
  sent_at    timestamptz not null default now(),
  reply_date date not null default now()::date,
  primary key (mailbox_id, sender, reply_date)
);

-- 12d inbound routing rules (first match wins, position order).
create table mail.mailbox_rule (
  id               uuid primary key,
  mailbox_id       uuid not null references mail.mailbox(id),
  position         integer not null default 0,
  enabled          boolean not null default true,
  match_from       text not null default '',   -- substring, case-insensitive; '' = no condition
  match_subject    text not null default '',   -- substring, case-insensitive; '' = no condition
  action_folder_id uuid references mail.folder(id),
  action_mark_read boolean not null default false,
  created_at       timestamptz not null default now()
);
