import { useState } from 'react';
import { api, ApiError } from '@/lib/api';
import { Button, Field, Input } from '@/design';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import { passwordScore } from '@/lib/validators';

export function PasswordTab() {
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const { score, hint } = passwordScore(next);
  const canSubmit = current && next && next === confirm && score >= 2;

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setErr(null);
    setBusy(true);
    try {
      await api.changePassword(current, next);
      toast('success', 'Password changed');
      setCurrent(''); setNext(''); setConfirm('');
    } catch (err) {
      setErr(err instanceof ApiError ? err.message : errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="max-w-md">
      <form onSubmit={submit} className="space-y-3">
        <Field label="Current password" htmlFor="pw-current">
          <Input id="pw-current" type="password" autoComplete="current-password" required value={current} onChange={(e) => setCurrent(e.target.value)} />
        </Field>
        <Field label="New password" hint={`Strength: ${hint}`} htmlFor="pw-new">
          <Input id="pw-new" type="password" autoComplete="new-password" required value={next} onChange={(e) => setNext(e.target.value)} />
        </Field>
        <Field
          label="Confirm new password"
          error={confirm && next !== confirm ? 'Passwords do not match' : undefined}
          htmlFor="pw-confirm"
        >
          <Input id="pw-confirm" type="password" autoComplete="new-password" required value={confirm} onChange={(e) => setConfirm(e.target.value)} />
        </Field>
        {err && <p className="text-sm text-danger" role="alert">{err}</p>}
        <Button type="submit" variant="primary" loading={busy} disabled={!canSubmit}>Change password</Button>
      </form>
    </div>
  );
}
