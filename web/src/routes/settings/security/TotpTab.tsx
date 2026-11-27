import { useState } from 'react';
import { api, ApiError } from '@/lib/api';
import { useAuth } from '@/stores/auth';
import { Button, ConfirmDialog, Field, Input, Modal } from '@/design';
import { CopyButton } from '@/components/CopyButton';
import { Icon } from '@/design/icons';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import type { RecoveryCodeSet } from '@/lib/types';

export function TotpTab() {
  const me = useAuth((s) => s.me);
  const setMe = useAuth((s) => s.setMe);
  const [setupOpen, setSetupOpen] = useState(false);
  const [disableOpen, setDisableOpen] = useState(false);
  const [regenOpen, setRegenOpen] = useState(false);

  if (!me) return null;
  const enabled = me.totp_enabled;

  return (
    <div className="max-w-xl space-y-4">
      <div className="flex items-start gap-4 p-4 border border-line rounded-lg">
        <span className={`w-9 h-9 rounded-full flex items-center justify-center shrink-0 ${enabled ? 'bg-success-soft text-success' : 'bg-warning-soft text-warning'}`}>
          <Icon name={enabled ? 'shield-check' : 'alert-triangle'} size={16} />
        </span>
        <div className="flex-1">
          <p className="font-medium">Two-factor authentication</p>
          <p className="text-sm text-ink-soft mt-1">
            {enabled
              ? 'Enabled. Your authenticator app supplies a 6-digit code at sign-in.'
              : 'Not enabled. Add a second factor so a compromised password is not enough to sign in.'}
          </p>
        </div>
        {enabled ? (
          <Button variant="danger" onClick={() => setDisableOpen(true)}>Disable</Button>
        ) : (
          <Button variant="primary" onClick={() => setSetupOpen(true)}>Set up</Button>
        )}
      </div>

      {enabled && (
        <div className="flex items-center justify-between p-4 border border-line rounded-lg">
          <div>
            <p className="font-medium text-sm">Recovery codes</p>
            <p className="text-xs text-ink-soft mt-0.5">One-time codes used if you lose access to your authenticator app.</p>
          </div>
          <Button variant="secondary" onClick={() => setRegenOpen(true)}>Regenerate</Button>
        </div>
      )}

      <TotpSetupModal
        open={setupOpen}
        onClose={() => setSetupOpen(false)}
        onEnabled={(updated) => {
          setMe(updated);
          setSetupOpen(false);
        }}
      />

      <DisableTotpModal
        open={disableOpen}
        onClose={() => setDisableOpen(false)}
        onDisabled={() => {
          setMe(me ? { ...me, totp_enabled: false } : null);
          setDisableOpen(false);
        }}
      />

      <RegenerateCodesModal
        open={regenOpen}
        onClose={() => setRegenOpen(false)}
      />
    </div>
  );
}

function TotpSetupModal({ open, onClose, onEnabled }: {
  open: boolean;
  onClose: () => void;
  onEnabled: (me: NonNullable<ReturnType<typeof useAuth.getState>['me']>) => void;
}) {
  const [setup, setSetup] = useState<{ secret: string; otpauth_url: string; qr_svg: string } | null>(null);
  const [code, setCode] = useState('');
  const [codes, setCodes] = useState<RecoveryCodeSet | null>(null);
  const [busy, setBusy] = useState(false);

  async function begin() {
    setBusy(true);
    try {
      const res = await api.totpSetup();
      setSetup(res);
    } catch (e) {
      toast('error', 'Could not start setup', errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function verify() {
    if (code.length !== 6) return;
    setBusy(true);
    try {
      const rec = await api.totpVerify(code);
      setCodes(rec);
      const me = await api.me();
      onEnabled(me);
    } catch (e) {
      toast('error', 'Invalid code', errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  function close() {
    setSetup(null); setCode(''); setCodes(null); setBusy(false);
    onClose();
  }

  // Kick off setup as soon as the modal opens.
  if (open && !setup && !busy) void begin();

  return (
    <Modal
      open={open}
      onClose={close}
      title={codes ? 'Save your recovery codes' : 'Set up two-factor authentication'}
      footer={
        codes ? (
          <Button variant="primary" onClick={close}>Done</Button>
        ) : (
          <>
            <Button variant="secondary" onClick={close}>Cancel</Button>
            <Button variant="primary" onClick={verify} loading={busy} disabled={code.length !== 6}>Verify</Button>
          </>
        )
      }
    >
      {codes ? (
        <div className="space-y-3">
          <p className="text-sm">Store these somewhere safe. They will not be shown again.</p>
          <div className="grid grid-cols-2 gap-2 font-mono text-sm border border-line rounded p-3">
            {codes.codes.map((c, i) => (
              <div key={i} className="px-2 py-1 bg-canvas rounded text-center">{c}</div>
            ))}
          </div>
          <CopyButton value={codes.codes.join('\n')} label="Copy all codes" />
        </div>
      ) : setup ? (
        <div className="space-y-4">
          <p className="text-sm text-ink-soft">
            Scan this QR code with an authenticator app (Google Authenticator, Microsoft Authenticator, 1Password, Authy).
          </p>
          <div className="flex justify-center" dangerouslySetInnerHTML={{ __html: setup.qr_svg }} />
          <div className="text-xs text-ink-soft">
            Or enter the secret manually: <span className="font-mono">{setup.secret}</span>
            <CopyButton value={setup.secret} small />
          </div>
          <Field label="6-digit code" htmlFor="totp-code">
            <Input
              id="totp-code"
              inputMode="numeric"
              autoComplete="one-time-code"
              pattern="[0-9]*"
              maxLength={6}
              value={code}
              onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))}
              autoFocus
            />
          </Field>
        </div>
      ) : (
        <div className="p-4 text-sm text-ink-soft flex items-center gap-2">
          <Icon name="info" size={14} /> Preparing setup…
        </div>
      )}
    </Modal>
  );
}

function DisableTotpModal({ open, onClose, onDisabled }: { open: boolean; onClose: () => void; onDisabled: () => void }) {
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);

  async function submit() {
    setBusy(true);
    try {
      await api.totpDisable(password);
      toast('success', 'Two-factor disabled');
      onDisabled();
    } catch (e) {
      toast('error', 'Could not disable', e instanceof ApiError ? e.message : errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Disable two-factor authentication?"
      description="Anyone with your password will be able to sign in. We strongly recommend keeping this on."
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>Cancel</Button>
          <Button variant="danger" onClick={submit} loading={busy} disabled={!password}>Disable</Button>
        </>
      }
    >
      <Field label="Confirm your password" htmlFor="totp-disable-pw">
        <Input id="totp-disable-pw" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} autoFocus />
      </Field>
    </Modal>
  );
}

function RegenerateCodesModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [codes, setCodes] = useState<RecoveryCodeSet | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit() {
    setBusy(true);
    try {
      const res = await api.regenerateRecoveryCodes();
      setCodes(res);
    } catch (e) {
      toast('error', 'Could not regenerate', errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  function close() { setCodes(null); onClose(); }

  return (
    <Modal
      open={open}
      onClose={close}
      title={codes ? 'New recovery codes' : 'Regenerate recovery codes?'}
      description={!codes ? 'Your existing recovery codes will stop working immediately.' : undefined}
      footer={
        codes ? <Button variant="primary" onClick={close}>Done</Button> : (
          <>
            <Button variant="secondary" onClick={close}>Cancel</Button>
            <Button variant="primary" onClick={submit} loading={busy}>Regenerate</Button>
          </>
        )
      }
    >
      {codes ? (
        <div className="space-y-3">
          <p className="text-sm">Store these somewhere safe. They will not be shown again.</p>
          <div className="grid grid-cols-2 gap-2 font-mono text-sm border border-line rounded p-3">
            {codes.codes.map((c, i) => <div key={i} className="px-2 py-1 bg-canvas rounded text-center">{c}</div>)}
          </div>
          <CopyButton value={codes.codes.join('\n')} label="Copy all codes" />
        </div>
      ) : null}
    </Modal>
  );
}

// Suppress unused import warning for ConfirmDialog.
void ConfirmDialog;
