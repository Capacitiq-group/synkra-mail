# Synkra Mail — web

React 18 + TypeScript + Vite + Tailwind SPA: the webmail (mail core, composer,
signatures, rules, autoresponder) and the org console (domains, users, webhooks,
API keys, deliverability). One origin with the Go API — in dev via the Vite
proxy, in prod behind the same reverse proxy. No CORS.

## Contract

Every endpoint, request and response shape in `src/types/api.ts` mirrors the Go
handlers in `../internal/api/` — that file is the binding spec; change the Go
side and it changes here too. Notable contract behaviour:

- **Bearer auth**: the API requires `Authorization: Bearer <token>` on every
  session route (the login response carries the token). The SPA keeps it in
  `localStorage` and wipes it on any 401.
- **Errors** are always `{error:{code,message,request_id,hint?}}`; the client
  unwraps them into `ApiError` and surfaces `hint` verbatim (lockout and
  suspended-org flows depend on it).
- **Idempotency-Key**: `/send`, `POST /addresses`, `POST /domains` replay on
  retry. The composer holds one UUID per draft; the offline outbox replays the
  same key until the send is accepted.
- **Attachments** are base64 `content` fields inside the send JSON — no
  multipart. Downloads go through `download_path` with the bearer header.

## Develop

```bash
source ../env.sh            # Go toolchain + database URL
npm install
npm run dev                 # http://localhost:5173, proxies /api → 127.0.0.1:8080
```

The Go server, tracker and (optional) DNS daemon run as usual from the repo root
(`make dev` or the systemd units).

## Build

```bash
npm run build               # tsc -b && vite build → dist/
```

`dist/` is static — serve it from the same host as `/api/v1` (nginx/Caddy) with
a fallback to `index.html` for client-side routes.

## PWA

`public/sw.js` caches the app shell only — never `/api/*`. Offline mail data is
served from the in-memory store; sends made offline queue in the outbox
(`src/lib/outbox.ts`) and flush with their original Idempotency-Key when the
connection returns, so the backend's idempotent replay dedupes them.

## Deliberately absent (tracked in BUILD_REGISTER.md)

- Server-side drafts (autosave is local; there is no drafts endpoint yet)
- WebSocket/SSE push (the API has no browser push channel — folders poll)
- WebAuthn passkeys (the backend enrols TOTP only)
- Notes list view (notes can be written per thread, there is no read endpoint)
- The staff/admin portal (Section 11) — separate surface, same stack
