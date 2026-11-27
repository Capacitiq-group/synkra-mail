import { useState } from 'react';
import { api } from '@/lib/api';
import { useAuth } from '@/stores/auth';
import { useDeletePasskey, usePasskeys } from '@/hooks/queries/security';
import { Button, ConfirmDialog, EmptyState, Field, Input, Skeleton } from '@/design';
import { Icon } from '@/design/icons';
import {
  base64urlToBuffer, bufferToBase64url, isWebAuthnAvailable, serializeAttestationCredential,
} from '@/lib/webauthn';
import { formatDateTime } from '@/lib/time';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import type { Passkey } from '@/lib/types';

export function PasskeysTab() {
  const me = useAuth((s) => s.me);
  const setMe = useAuth((s) => s.setMe);
  const q = usePasskeys();
  const del = useDeletePasskey();
  const [label, setLabel] = useState('');
  const [busy, setBusy] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<Passkey | null>(null);

  const supported = isWebAuthnAvailable();

  async function addPasskey() {
    if (!supported) { toast('error', 'This browser does not support passkeys.'); return; }
    if (!label.trim()) { toast('error', 'Give the passkey a name'); return; }
    setBusy(true);
    try {
      const begin = await api.beginPasskeyRegister(label.trim());
      const credential = await navigator.credentials.create({
        publicKey: {
          challenge: base64urlToBuffer(begin.challenge),
          rp: { id: begin.rp.id, name: begin.rp.name },
          user: {
            id: base64urlToBuffer(begin.user.id),
            name: begin.user.name,
            displayName: begin.user.displayName,
          },
          pubKeyCredParams: [
            { type: 'public-key', alg: -7 },   // ES256
            { type: 'public-key', alg: -257 }, // RS256
          ],
          authenticatorSelection: {
            residentKey: 'preferred',
            userVerification: 'preferred',
          },
          attestation: 'none',
          timeout: 60_000,
        },
      });
      if (!credential) throw new Error('Registration cancelled');
      const serialized = serializeAttestationCredential(credential as PublicKeyCredential);
      await api.finishPasskeyRegister(serialized, begin.challenge, label.trim());
      toast('success', 'Passkey registered');
      setLabel('');
      await q.refetch();
      const updatedMe = await api.me();
      setMe(updatedMe);
    } catch (e) {
      if ((e as { name?: string }).name === 'NotAllowedError') {
        toast('error', 'Passkey registration cancelled');
      } else {
        toast('error', 'Registration failed', errorMessage(e));
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="max-w-2xl space-y-4">
      <p className="text-sm text-ink-soft">
        Sign in without typing a password. Passkeys use your device's built-in authenticator (Face ID, Touch ID, Windows Hello, security key).
      </p>

      {!supported && (
        <div className="border border-warning/40 bg-warning-soft rounded p-3 text-sm">
          <Icon name="alert-triangle" size={14} className="inline mr-1" />
          This browser does not support passkeys. Try a recent version of Chrome, Safari, Edge or Firefox.
        </div>
      )}

      <div className="flex flex-wrap items-end gap-2 border border-line rounded-lg p-4">
        <div className="flex-1 min-w-[200px]">
          <Field label="Passkey name" htmlFor="passkey-label">
            <Input id="passkey-label" value={label} onChange={(e) => setLabel(e.target.value)} placeholder="e.g. MacBook Touch ID" />
          </Field>
        </div>
        <Button variant="primary" onClick={addPasskey} loading={busy} disabled={!supported || !label.trim()}>
          <Icon name="lock" size={14} /> Add passkey
        </Button>
      </div>

      {q.isLoading && <Skeleton className="h-20 w-full" />}
      {q.data && q.data.length === 0 && (
        <EmptyState
          icon={<Icon name="lock" size={28} />}
          title="No passkeys yet"
          description="Add at least one so you can sign in without a password."
        />
      )}
      {q.data && q.data.length > 0 && (
        <ul className="space-y-2">
          {q.data.map((p) => (
            <li key={p.id} className="flex items-center gap-3 p-3 border border-line rounded">
              <Icon name="lock" size={16} className="text-accent" />
              <div className="flex-1 min-w-0">
                <p className="text-sm font-medium truncate">{p.label}</p>
                <p className="text-xs text-ink-soft">
                  Added {formatDateTime(p.created_at)}
                  {p.last_used_at && ` · last used ${formatDateTime(p.last_used_at)}`}
                </p>
              </div>
              <Button size="sm" variant="ghost" onClick={() => setDeleteTarget(p)}>Remove</Button>
            </li>
          ))}
        </ul>
      )}

      <ConfirmDialog
        open={!!deleteTarget}
        onCancel={() => setDeleteTarget(null)}
        onConfirm={async () => {
          if (!deleteTarget) return;
          try {
            await del.mutateAsync(deleteTarget.id);
            toast('success', 'Passkey removed');
            setDeleteTarget(null);
            if (me) setMe({ ...me, passkeys: Math.max(0, me.passkeys - 1) });
          } catch (e) {
            toast('error', 'Could not remove passkey', errorMessage(e));
          }
        }}
        title={`Remove "${deleteTarget?.label}"?`}
        description="You will no longer be able to use this passkey to sign in."
        confirmLabel="Remove"
        danger
        busy={del.isPending}
      />
    </div>
  );
}

// Suppress unused import warning.
void bufferToBase64url;
