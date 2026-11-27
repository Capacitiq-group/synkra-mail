import { useState, type FormEvent } from 'react';
import { Link, useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { api, ApiError } from '@/lib/api';
import { useAuth } from '@/stores/auth';
import { toast } from '@/stores/toast';
import { Button, Field, Input, Spinner } from '@/design';
import { usePageTitle } from '@/hooks';
import { isEmail, passwordScore } from '@/lib/validators';
import { looksLikeVerificationToken } from '@/lib/validators';

// ===== Login =====
export function LoginRoute() {
  usePageTitle('Sign in');
  const navigate = useNavigate();
  const location = useLocation();
  const setMe = useAuth((s) => s.setMe);
  const me = useAuth((s) => s.me);

  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [phase, setPhase] = useState<'password' | 'totp' | 'passkey'>('password');
  const [challenge, setChallenge] = useState('');
  const [totpCode, setTotpCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const from = (location.state as { from?: { pathname: string } } | null)?.from?.pathname ?? '/';
  if (me) { navigate(from, { replace: true }); return null; }

  async function submitPassword(e: FormEvent) {
    e.preventDefault();
    setErr(null); setBusy(true);
    try {
      const res = await api.login(email.trim().toLowerCase(), password);
      if (res.requires_totp && res.totp_challenge) { setChallenge(res.totp_challenge); setPhase('totp'); return; }
      if (res.requires_passkey && res.passkey_challenge) { setChallenge(res.passkey_challenge.challenge); setPhase('passkey'); return; }
      if (res.me) { setMe(res.me); navigate(from, { replace: true }); }
    } catch (e) {
      const status = e instanceof ApiError ? e.status : 0;
      setErr(status === 429 ? 'Too many attempts. Try again in a few minutes.' : (e instanceof ApiError ? e.message : 'Login failed'));
    } finally { setBusy(false); }
  }

  async function submitTotp(e: FormEvent) {
    e.preventDefault();
    setErr(null); setBusy(true);
    try {
      const res = await api.loginTotp(challenge, totpCode.trim());
      if (res.me) { setMe(res.me); navigate(from, { replace: true }); }
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'Invalid code');
    } finally { setBusy(false); }
  }

  async function startPasskey() {
    setErr(null); setBusy(true);
    try {
      const begin = await api.beginPasskeyLogin(email.trim().toLowerCase());
      if (!begin.passkey_challenge || !('credentials' in navigator)) {
        setErr('No passkey available for this account on this device.');
        return;
      }
      const challenge = begin.passkey_challenge.challenge;
      const credential = await navigator.credentials.get({
        publicKey: {
          challenge: base64urlToBuffer(challenge),
          timeout: 60_000,
          userVerification: 'preferred',
          allowCredentials: begin.passkey_challenge.allow_credentials.map((c) => ({ type: 'public-key' as const, id: base64urlToBuffer(c.id) })),
        },
      });
      const presp = (credential as PublicKeyCredential).response;
      const res = await api.finishPasskeyLogin({
        id: bufferToBase64url((credential as PublicKeyCredential).rawId),
        clientDataJSON: bufferToBase64url(presp instanceof AuthenticatorAssertionResponse ? presp.clientDataJSON : new ArrayBuffer(0)),
        authenticatorData: bufferToBase64url(presp instanceof AuthenticatorAssertionResponse ? presp.authenticatorData : new ArrayBuffer(0)),
        signature: bufferToBase64url(presp instanceof AuthenticatorAssertionResponse ? presp.signature : new ArrayBuffer(0)),
        userHandle: presp instanceof AuthenticatorAssertionResponse && presp.userHandle
          ? bufferToBase64url(presp.userHandle) : null,
      }, challenge);
      if (res.me) { setMe(res.me); navigate(from, { replace: true }); }
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'Passkey authentication failed or was cancelled.');
    } finally { setBusy(false); }
  }

  return (
    <div className="min-h-full flex items-center justify-center p-6 bg-canvas">
      <div className="w-full max-w-sm bg-surface rounded-lg p-6 shadow-sm border border-line">
        <div className="text-center mb-6">
          <div className="w-12 h-12 rounded-lg bg-accent text-white flex items-center justify-center text-xl font-bold mx-auto">S</div>
          <h1 className="mt-3 text-lg font-semibold">Sign in to Synkra Mail</h1>
          <p className="text-sm text-ink-soft mt-1">Use your work email address.</p>
        </div>

        {phase === 'password' && (
          <form onSubmit={submitPassword} className="space-y-3">
            <Field label="Email" htmlFor="login-email">
              <Input id="login-email" type="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} />
            </Field>
            <Field label="Password" htmlFor="login-password">
              <Input id="login-password" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
            </Field>
            {err && <p role="alert" className="text-sm text-danger">{err}</p>}
            <Button type="submit" variant="primary" className="w-full" loading={busy}>
              {busy ? 'Signing in…' : 'Sign in'}
            </Button>
            <div className="flex items-center justify-between text-sm pt-1">
              <Link to="/forgot-password" className="text-ink-soft hover:text-ink">Forgot password?</Link>
              {email && <button type="button" onClick={startPasskey} disabled={busy || !isEmail(email)} className="text-accent hover:underline">Use a passkey</button>}
            </div>
          </form>
        )}

        {phase === 'totp' && (
          <form onSubmit={submitTotp} className="space-y-3">
            <p className="text-sm text-ink-soft">Enter the 6-digit code from your authenticator app.</p>
            <Field label="Authentication code" htmlFor="login-totp">
              <Input id="login-totp" inputMode="numeric" pattern="[0-9]*" maxLength={6} autoComplete="one-time-code" required value={totpCode} onChange={(e) => setTotpCode(e.target.value.replace(/\D/g, ''))} />
            </Field>
            {err && <p role="alert" className="text-sm text-danger">{err}</p>}
            <Button type="submit" variant="primary" className="w-full" loading={busy} disabled={totpCode.length !== 6}>Verify</Button>
            <button type="button" onClick={() => { setPhase('password'); setErr(null); }} className="text-sm text-ink-soft w-full text-center">Use password instead</button>
          </form>
        )}

        {phase === 'passkey' && (
          <div className="space-y-3">
            <p className="text-sm text-ink-soft">Authenticate with your passkey.</p>
            {err && <p role="alert" className="text-sm text-danger">{err}</p>}
            <Button variant="primary" className="w-full" loading={busy} onClick={startPasskey}>Continue</Button>
            <button type="button" onClick={() => { setPhase('password'); setErr(null); }} className="text-sm text-ink-soft w-full text-center">Use password instead</button>
          </div>
        )}

        <p className="text-xs text-ink-mute text-center mt-6">
          By signing in you accept the operator agreement and privacy notice.
        </p>
      </div>
    </div>
  );
}

// ===== Logout =====
export function LogoutRoute() {
  usePageTitle('Signing out');
  const navigate = useNavigate();
  const setMe = useAuth((s) => s.setMe);
  useQuery({
    queryKey: ['logout'],
    queryFn: async () => { try { await api.logout(); } finally { setMe(null); navigate('/login', { replace: true }); } return true; },
    retry: false, staleTime: Infinity,
  });
  return <div className="min-h-full flex items-center justify-center"><Spinner size={24} /></div>;
}

// ===== Forgot password =====
export function ForgotPasswordRoute() {
  usePageTitle('Forgot password');
  const [email, setEmail] = useState('');
  const [sent, setSent] = useState(false);
  const [busy, setBusy] = useState(false);
  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    try { await api.forgotPassword(email.trim().toLowerCase()); setSent(true); }
    catch { setSent(true); /* never leak existence of account */ }
    finally { setBusy(false); }
  }
  if (sent) {
    return (
      <div className="min-h-full flex items-center justify-center p-6 bg-canvas">
        <div className="max-w-sm w-full bg-surface rounded-lg p-6 shadow-sm border border-line text-center">
          <h1 className="font-semibold text-lg">Check your email</h1>
          <p className="text-sm text-ink-soft mt-2">If an account exists for {email}, you will receive a password reset link shortly.</p>
          <Link to="/login" className="inline-block mt-4 text-accent text-sm hover:underline">Back to sign in</Link>
        </div>
      </div>
    );
  }
  return (
    <div className="min-h-full flex items-center justify-center p-6 bg-canvas">
      <form onSubmit={submit} className="w-full max-w-sm bg-surface rounded-lg p-6 shadow-sm border border-line space-y-3">
        <h1 className="font-semibold text-lg">Reset your password</h1>
        <p className="text-sm text-ink-soft">We'll email you a link to reset it.</p>
        <Field label="Email" htmlFor="forgot-email">
          <Input id="forgot-email" type="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
        </Field>
        <Button type="submit" variant="primary" className="w-full" loading={busy}>Send reset link</Button>
        <Link to="/login" className="block text-center text-sm text-ink-soft">Back to sign in</Link>
      </form>
    </div>
  );
}

// ===== Reset password =====
export function ResetPasswordRoute() {
  usePageTitle('Reset password');
  const [params] = useSearchParams();
  const token = params.get('token') ?? '';
  const navigate = useNavigate();
  const [pw, setPw] = useState('');
  const [confirm, setConfirm] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const { score, hint } = passwordScore(pw);

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (pw !== confirm) { setErr('Passwords do not match'); return; }
    if (score < 2) { setErr('Choose a stronger password'); return; }
    setBusy(true); setErr(null);
    try {
      await api.resetPassword(token, pw);
      toast('success', 'Password reset. Please sign in.');
      navigate('/login', { replace: true });
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'Could not reset password');
    } finally { setBusy(false); }
  }

  if (!looksLikeVerificationToken(token)) {
    return (
      <div className="min-h-full flex items-center justify-center p-6 bg-canvas">
        <div className="max-w-sm w-full bg-surface rounded-lg p-6 border border-line text-center">
          <p className="text-sm">This reset link is invalid or has expired.</p>
          <Link to="/forgot-password" className="inline-block mt-4 text-accent text-sm hover:underline">Request a new link</Link>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-full flex items-center justify-center p-6 bg-canvas">
      <form onSubmit={submit} className="w-full max-w-sm bg-surface rounded-lg p-6 shadow-sm border border-line space-y-3">
        <h1 className="font-semibold text-lg">Set a new password</h1>
        <Field label="New password" hint={`Strength: ${hint}`}>
          <Input type="password" autoComplete="new-password" required value={pw} onChange={(e) => setPw(e.target.value)} />
        </Field>
        <Field label="Confirm password" error={confirm && pw !== confirm ? 'Passwords do not match' : undefined}>
          <Input type="password" autoComplete="new-password" required value={confirm} onChange={(e) => setConfirm(e.target.value)} />
        </Field>
        {err && <p role="alert" className="text-sm text-danger">{err}</p>}
        <Button type="submit" variant="primary" className="w-full" loading={busy} disabled={pw !== confirm || score < 2}>Reset password</Button>
      </form>
    </div>
  );
}

// ===== Not found =====
export function NotFoundRoute() {
  usePageTitle('Not found');
  return (
    <div className="min-h-full flex items-center justify-center p-6 bg-canvas">
      <div className="text-center">
        <p className="text-4xl font-bold">404</p>
        <p className="text-sm text-ink-soft mt-2">We could not find that page.</p>
        <Link to="/" className="inline-block mt-4 px-3 py-1.5 rounded bg-accent text-white text-sm">Back to mail</Link>
      </div>
    </div>
  );
}

// ===== Helpers =====
function base64urlToBuffer(s: string): ArrayBuffer {
  const pad = '='.repeat((4 - (s.length % 4)) % 4);
  const b64 = (s + pad).replace(/-/g, '+').replace(/_/g, '/');
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out.buffer;
}
function bufferToBase64url(buf: ArrayBuffer): string {
  const bin = String.fromCharCode(...new Uint8Array(buf));
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}
