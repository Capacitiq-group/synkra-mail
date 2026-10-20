-- v1.2 owner decision: open/click tracking is optional and OFF by default
-- (per-org toggle; per-message opt-out in compose).
alter table mail.org add column if not exists tracking_enabled boolean not null default false;

-- Plan limits for the tracker URL config surfaced in API responses.
alter table mail.org add column if not exists tracking_started_at timestamptz;
