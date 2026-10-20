-- Product invariant backfill: user-kind addresses are always sendable.
update mail.address set allow_send_as = true where kind = 'user' and allow_send_as = false;
