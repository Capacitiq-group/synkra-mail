// DomainDashboardRoute — the registrar-managed domain dashboard (phase 11):
// lifecycle banner (grace/redemption), DNS doctor with per-provider
// instructions and copy buttons, nameservers (full vs partially managed),
// transfer lock, WHOIS privacy, auto-renew, EPP retrieval behind a password
// step-up, registrant contacts, renew, status sync, transfer-out.
import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import { usePageTitle } from '@/hooks';
import { Button, Field, Input, Modal, Skeleton } from '@/design';
import { Banner, Panel } from '@/components/Banner';
import { CopyButton } from '@/components/CopyButton';
import { providerInstructions } from './providerInstructions';
import type { DomainDetail, DoctorCheck, RegistrantContact } from '@/lib/types';

const NS_SYNKRA_HINT = ['ns1.mx1.synkra.co.za', 'ns2.mx1.synkra.co.za'];

export function DomainDashboardRoute() {
  const { domainId = '' } = useParams();
  usePageTitle('Domain');
  const qc = useQueryClient();
  const detailQ = useQuery({
    queryKey: ['domain-detail', domainId],
    queryFn: () => api.domainDetail(domainId),
    staleTime: 30_000,
  });
  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['domain-detail', domainId] });
    qc.invalidateQueries({ queryKey: ['domains'] });
  };

  const [nsOpen, setNsOpen] = useState(false);
  const [eppOpen, setEppOpen] = useState(false);
  const [registrantOpen, setRegistrantOpen] = useState(false);

  if (detailQ.isLoading) return <div className="p-6"><Skeleton className="h-64 w-full" /></div>;
  if (detailQ.isError || !detailQ.data) {
    return (
      <div className="p-6">
        <p className="text-sm text-danger">Could not load the domain. <Link className="underline" to="/settings/domains">Back to the list.</Link></p>
      </div>
    );
  }
  const detail = detailQ.data;
  const reg = detail.registrar;

  return (
    <div className="p-4 md:p-6 max-w-4xl mx-auto space-y-4">
      <div className="flex flex-wrap items-center gap-3 justify-between">
        <div>
          <h1 className="text-lg font-semibold">{detail.domain.name}</h1>
          <p className="text-sm text-ink-soft">
            Registered {reg.registered_at ? new Date(reg.registered_at).toLocaleDateString('en-ZA') : '—'} ·
            renews until {reg.renews_until ? new Date(reg.renews_until).toLocaleDateString('en-ZA') : '—'} ·
            registrar: {reg.registrar === 'oner' ? 'Oner (ZARC)' : reg.registrar}
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="ghost" onClick={() => api.syncDomainStatus(domainId).then(invalidate).catch((e) => toast('error', 'Sync failed', errorMessage(e)))}>
            Refresh from registrar
          </Button>
          <Button variant="primary" onClick={() => api.renewDomain(domainId, 1).then(() => { toast('success', 'Renewed for 1 year'); invalidate(); }).catch((e) => toast('error', 'Renewal failed', errorMessage(e)))}>
            Renew +1 year
          </Button>
        </div>
      </div>

      <LifecycleBanner detail={detail} />
      {detail.partially_managed && (
        <Banner
          tone="info"
          icon="info"
          title="Partially managed"
          body="The domain is registered through us, but DNS runs on your own nameservers. Publish the mail records below at your provider — the doctor checks them either way."
        />
      )}

      <DnsDoctorCard domainId={domainId} />
      <NameserversCard detail={detail} onEdit={() => setNsOpen(true)} />
      <RegistrarActionsCard
        detail={detail}
        onLock={(v) => api.toggleDomainFlag(domainId, 'transfer-lock', v).then(invalidate).catch((e) => toast('error', 'Failed', errorMessage(e)))}
        onPrivacy={(v) => api.toggleDomainFlag(domainId, 'whois-privacy', v).then(invalidate).catch((e) => toast('error', 'Failed', errorMessage(e)))}
        onAutoRenew={(v) => api.toggleDomainFlag(domainId, 'auto-renew', v).then(invalidate).catch((e) => toast('error', 'Failed', errorMessage(e)))}
        onEpp={() => setEppOpen(true)}
        onRegistrant={() => setRegistrantOpen(true)}
        onTransferOut={(code) => api.transferOut(domainId, code).then(() => { toast('success', 'Transfer started'); invalidate(); }).catch((e) => toast('error', 'Transfer failed', errorMessage(e)))}
      />

      {nsOpen && (
        <NameserversModal
          domainId={domainId}
          current={reg.nameservers}
          managed={reg.managed_mode}
          onClose={() => setNsOpen(false)}
          onSaved={() => { setNsOpen(false); invalidate(); }}
        />
      )}
      {eppOpen && (
        <EppModal
          domainId={domainId}
          domain={detail.domain.name}
          onClose={() => setEppOpen(false)}
          onTransferOut={(code) => { setEppOpen(false); api.transferOut(domainId, code).then(() => { toast('success', 'Transfer started'); invalidate(); }).catch((e) => toast('error', 'Transfer failed', errorMessage(e))); }}
        />
      )}
      {registrantOpen && (
        <RegistrantModal
          domainId={domainId}
          current={reg.registrant}
          onClose={() => setRegistrantOpen(false)}
          onSaved={() => { setRegistrantOpen(false); invalidate(); }}
        />
      )}
    </div>
  );
}

function LifecycleBanner({ detail }: { detail: DomainDetail }) {
  const { registrar: reg, grace_note } = detail;
  const tone = reg.lifecycle === 'ok' || reg.lifecycle === 'add_grace' || reg.lifecycle === 'renew_grace'
    ? (reg.registry_status === 'ok' ? 'success' : 'info')
    : 'danger';
  if (tone === 'success' && reg.registry_status === 'ok' && reg.lifecycle === 'ok') {
    return (
      <Banner tone="success" icon="shield-check" title="In good standing"
        body={`Registry status ok${reg.auto_renew ? ' · auto-renew on' : ' · auto-renew OFF'}`} />
    );
  }
  return (
    <Banner
      tone={tone as 'success' | 'info' | 'danger'}
      icon={tone === 'danger' ? 'alert-triangle' : 'info'}
      title={lifecycleTitle(reg.registry_status, reg.lifecycle)}
      body={[lifecycleBody(reg.registry_status), grace_note].filter(Boolean).join(' ')}
    />
  );
}

function lifecycleTitle(status: string, lifecycle: string): string {
  if (status === 'pending_create') return 'Awaiting registry confirmation (ZARC)';
  if (status === 'pending_update') return 'Contact update pending verification';
  if (status === 'client_hold' || status === 'server_hold') return 'Registry hold on this domain';
  if (lifecycle === 'redemption') return 'Redemption period';
  if (lifecycle === 'pending_delete') return 'Pending delete';
  if (lifecycle === 'expired') return 'Expired';
  return 'Add grace period';
}

function lifecycleBody(status: string): string {
  if (status === 'pending_create') return 'The EPP create command is in flight at the registry — this usually clears within minutes.';
  if (status === 'pending_update') return 'The registrant change is pending; the registry e-mails the contact for approval.';
  return '';
}

// ---- DNS doctor ----

function DnsDoctorCard({ domainId }: { domainId: string }) {
  const doctorQ = useQuery({
    queryKey: ['domain-doctor', domainId],
    queryFn: () => api.dnsDoctor(domainId),
    staleTime: 60_000,
  });
  const [openRecord, setOpenRecord] = useState<string | null>(null);
  const [showAllProviders, setShowAllProviders] = useState(false);
  const [chosenProvider, setChosenProvider] = useState<string>('konsoleH');

  return (
    <Panel>
      <div className="flex items-center justify-between">
        <div>
          <h2 className="font-medium">DNS doctor</h2>
          <p className="text-xs text-ink-soft">Expected mail records with live verdicts — run against the public DNS.</p>
        </div>
        <Button variant="ghost" onClick={() => doctorQ.refetch()}>{doctorQ.isFetching ? 'Checking…' : 'Re-check'}</Button>
      </div>

      {doctorQ.isLoading && <Skeleton className="h-24 w-full" />}
      {doctorQ.data?.checks.map((check) => (
        <DoctorRow key={check.record} check={check} onHelp={() => setOpenRecord(openRecord === check.record ? null : check.record)} />
      ))}

      {doctorQ.data?.records.map((r) => {
        const key = `${r.type} ${r.name}`;
        return (
          <div key={key} className="border border-line rounded p-3 text-sm space-y-2">
            <div className="flex items-center justify-between gap-2">
              <div className="font-medium">{key} <span className="text-ink-soft font-normal">· {r.purpose}</span></div>
              <Button variant="ghost" onClick={() => setOpenRecord(openRecord === key ? null : key)}>
                {openRecord === key ? 'Hide setup' : 'How to publish'}
              </Button>
            </div>
            <div className="flex items-center gap-2 bg-canvas rounded p-2 font-mono text-xs">
              <span className="flex-1 break-all">{r.value}</span>
              <CopyButton value={r.value} />
            </div>
            {openRecord === key && (
              <div className="space-y-2 pt-1">
                <div className="flex flex-wrap gap-1">
                  {['konsoleH', 'ClientZone', 'Domains.co.za', 'Truehost', 'Cloudflare'].map((p) => (
                    <button
                      key={p}
                      type="button"
                      onClick={() => setChosenProvider(p)}
                      className={`text-xs px-2 py-1 rounded ${chosenProvider === p ? 'bg-accent text-white' : 'bg-canvas text-ink-soft'}`}
                    >
                      {p}
                    </button>
                  ))}
                  <button
                    type="button"
                    onClick={() => setShowAllProviders(!showAllProviders)}
                    className="text-xs px-2 py-1 rounded bg-canvas text-ink-soft"
                  >
                    {showAllProviders ? 'Show one' : 'Show all'}
                  </button>
                </div>
                {providerInstructions(r)
                  .filter((pi) => showAllProviders || pi.provider === chosenProvider)
                  .map((pi) => (
                    <div key={pi.provider} className="bg-canvas rounded p-3">
                      <p className="text-xs font-medium">{pi.provider} <span className="text-ink-mute">({pi.portal})</span></p>
                      <ol className="list-decimal ml-4 text-xs text-ink-soft space-y-0.5 mt-1">
                        {pi.steps.map((s, i) => <li key={i}>{s}</li>)}
                      </ol>
                    </div>
                  ))}
              </div>
            )}
          </div>
        );
      })}
    </Panel>
  );
}

function DoctorRow({ check, onHelp }: { check: DoctorCheck; onHelp: () => void }) {
  const tone = check.verdict === 'pass' ? 'text-success' : check.verdict === 'fail' ? 'text-danger' : 'text-ink-soft';
  return (
    <div className="flex items-center justify-between gap-3 text-sm border-b border-line pb-2">
      <div className="min-w-0">
        <span className={`font-medium ${tone}`}>{check.verdict === 'pass' ? '✓' : check.verdict === 'fail' ? '✕' : '?'} {check.record}</span>
        <span className="text-ink-soft ml-2 truncate">{check.actual ? check.actual.slice(0, 60) : 'no record found'}</span>
      </div>
      {check.verdict !== 'pass' && <Button variant="ghost" onClick={onHelp}>Fix</Button>}
    </div>
  );
}

// ---- nameservers ----

function NameserversCard({ detail, onEdit }: { detail: DomainDetail; onEdit: () => void }) {
  const reg = detail.registrar;
  return (
    <Panel>
      <div className="flex items-center justify-between">
        <h2 className="font-medium">Nameservers</h2>
        <Button variant="ghost" onClick={onEdit}>Change</Button>
      </div>
      <ul className="text-sm font-mono text-ink-soft">
        {(reg.nameservers.length ? reg.nameservers : ['(not delegated)']).map((ns) => <li key={ns}>{ns}</li>)}
      </ul>
      <p className="text-xs text-ink-soft">
        {reg.managed_mode === 'full' && 'Our authoritative nameservers serve the mail-ready zone for this domain.'}
        {reg.managed_mode === 'partial' && 'Your nameservers are authoritative; the mail records must exist there (doctor above).'}
        {reg.managed_mode === 'unmanaged' && 'No delegation on file yet.'}
      </p>
    </Panel>
  );
}

function NameserversModal({ domainId, current, managed, onClose, onSaved }: {
  domainId: string; current: string[]; managed: string; onClose: () => void; onSaved: () => void;
}) {
  const [mode, setMode] = useState<'synkra' | 'custom'>(managed === 'partial' ? 'custom' : 'synkra');
  const [ns1, setNs1] = useState(current[0] ?? '');
  const [ns2, setNs2] = useState(current[1] ?? '');
  const m = useMutation({
    mutationFn: () => api.setNameservers(domainId, mode === 'synkra' ? NS_SYNKRA_HINT : [ns1, ns2]),
    onSuccess: () => { toast('success', 'Nameservers updated at the registrar'); onSaved(); },
    onError: (e) => toast('error', 'Update failed', errorMessage(e)),
  });
  return (
    <Modal open onClose={onClose} title="Nameservers" size="lg">
      <div className="space-y-3">
        <label className="flex items-start gap-3 p-3 border border-line rounded cursor-pointer">
          <input type="radio" checked={mode === 'synkra'} onChange={() => setMode('synkra')} className="mt-1" />
          <span><span className="font-medium block">Synkra's nameservers</span><span className="text-xs text-ink-soft">Full management — we serve the zone.</span></span>
        </label>
        <label className="flex items-start gap-3 p-3 border border-line rounded cursor-pointer">
          <input type="radio" checked={mode === 'custom'} onChange={() => setMode('custom')} className="mt-1" />
          <span><span className="font-medium block">My own nameservers</span><span className="text-xs text-ink-soft">Partially managed — publish the mail records yourself.</span></span>
        </label>
        {mode === 'custom' && (
          <div className="grid grid-cols-2 gap-3">
            <Field label="Nameserver 1"><Input value={ns1} onChange={(e) => setNs1(e.target.value)} /></Field>
            <Field label="Nameserver 2"><Input value={ns2} onChange={(e) => setNs2(e.target.value)} /></Field>
          </div>
        )}
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          <Button variant="primary" disabled={mode === 'custom' && (!ns1 || !ns2)} onClick={() => m.mutate()}>
            {m.isPending ? 'Saving…' : 'Save at registrar'}
          </Button>
        </div>
      </div>
    </Modal>
  );
}

// ---- registrar actions ----

function RegistrarActionsCard({ detail, onLock, onPrivacy, onAutoRenew, onEpp, onRegistrant, onTransferOut }: {
  detail: DomainDetail;
  onLock: (v: boolean) => void;
  onPrivacy: (v: boolean) => void;
  onAutoRenew: (v: boolean) => void;
  onEpp: () => void;
  onRegistrant: () => void;
  onTransferOut: (code: string) => void;
}) {
  const reg = detail.registrar;
  const [transferCode, setTransferCode] = useState('');
  const [transferOpen, setTransferOpen] = useState(false);

  return (
    <Panel>
      <h2 className="font-medium">Registrar settings</h2>
      <div className="grid md:grid-cols-3 gap-3">
        <Toggle label="Transfer lock" hint="Blocks unauthorised transfer-away" value={reg.transfer_lock} onChange={onLock} />
        <Toggle label="WHOIS privacy" hint="Masks registrant contact details" value={reg.whois_privacy} onChange={onPrivacy} />
        <Toggle label="Auto-renew" hint="Renews before expiry" value={reg.auto_renew} onChange={onAutoRenew} />
      </div>
      <div className="flex flex-wrap gap-2">
        <Button variant="ghost" onClick={onRegistrant}>Registrant contacts</Button>
        <Button variant="ghost" onClick={onEpp}>Get transfer code (EPP)</Button>
        {!transferOpen && <Button variant="ghost" onClick={() => setTransferOpen(true)}>Transfer away</Button>}
      </div>
      {transferOpen && (
        <div className="flex flex-wrap items-end gap-2 border border-danger/30 rounded p-3">
          <Field label="EPP code from the step-up dialog">
            <Input value={transferCode} onChange={(e) => setTransferCode(e.target.value)} />
          </Field>
          <Button variant="danger" disabled={!transferCode} onClick={() => { onTransferOut(transferCode); setTransferOpen(false); }}>
            Start transfer
          </Button>
        </div>
      )}
    </Panel>
  );
}

function Toggle({ label, hint, value, onChange }: { label: string; hint: string; value: boolean; onChange: (v: boolean) => void }) {
  return (
    <label className="flex items-start gap-3 p-3 border border-line rounded cursor-pointer">
      <input type="checkbox" checked={value} onChange={(e) => onChange(e.target.checked)} className="mt-1" />
      <span><span className="font-medium block text-sm">{label}</span><span className="text-xs text-ink-soft">{hint}</span></span>
    </label>
  );
}

// ---- EPP step-up ----

function EppModal({ domainId, domain, onClose, onTransferOut }: { domainId: string; domain: string; onClose: () => void; onTransferOut: (code: string) => void }) {
  const [password, setPassword] = useState('');
  const [code, setCode] = useState<string | null>(null);
  const m = useMutation({
    mutationFn: () => api.retrieveEPP(domainId, password),
    onSuccess: (c) => setCode(c),
    onError: (e) => toast('error', 'Retrieval failed', errorMessage(e)),
  });
  return (
    <Modal open onClose={onClose} title="Transfer authorisation code" size="lg">
      {!code ? (
        <div className="space-y-3">
          <p className="text-sm text-ink-soft">
            The EPP code authorises moving <span className="font-medium">{domain}</span> to another registrar. Confirm your
            password to continue — the retrieval is audited.
          </p>
          <Field label="Your password">
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={onClose}>Cancel</Button>
            <Button variant="primary" disabled={!password || m.isPending} onClick={() => m.mutate()}>
              {m.isPending ? 'Verifying…' : 'Confirm and retrieve'}
            </Button>
          </div>
        </div>
      ) : (
        <div className="space-y-3">
          <div className="flex items-center gap-2 bg-canvas rounded p-3 font-mono text-sm">
            <span className="flex-1 break-all">{code}</span>
            <CopyButton value={code} />
          </div>
          <p className="text-xs text-ink-soft">Keep the transfer lock on unless you are actively moving the domain.</p>
          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={onClose}>Done</Button>
            <Button variant="danger" onClick={() => onTransferOut(code)}>Start transfer with this code</Button>
          </div>
        </div>
      )}
    </Modal>
  );
}

// ---- registrant ----

function RegistrantModal({ domainId, current, onClose, onSaved }: {
  domainId: string; current?: Partial<RegistrantContact>; onClose: () => void; onSaved: () => void;
}) {
  const [c, setC] = useState<RegistrantContact>({
    organisation: current?.organisation ?? '',
    first_name: current?.first_name ?? '',
    last_name: current?.last_name ?? '',
    email: current?.email ?? '',
    phone: current?.phone ?? '',
    street: current?.street ?? '',
    city: current?.city ?? '',
    province: current?.province ?? '',
    postal_code: current?.postal_code ?? '',
    country_code: current?.country_code ?? 'ZA',
  });
  const m = useMutation({
    mutationFn: () => api.updateRegistrant(domainId, c),
    onSuccess: (r) => { toast('success', 'Registrant update sent', r.note); onSaved(); },
    onError: (e) => toast('error', 'Update failed', errorMessage(e)),
  });
  const f = (k: keyof RegistrantContact, label: string, ph?: string) => (
    <Field label={label}>
      <Input value={String(c[k] ?? '')} placeholder={ph} onChange={(e) => setC({ ...c, [k]: e.target.value })} />
    </Field>
  );
  return (
    <Modal open onClose={onClose} title="Registrant contacts" size="lg">
      <div className="space-y-3">
        <p className="text-sm text-ink-soft">The registrant is the legal holder — changes trigger a registry verification e-mail. WHOIS privacy can mask these details publicly.</p>
        <div className="grid grid-cols-2 gap-3">
          {f('first_name', 'First name')}
          {f('last_name', 'Last name')}
          {f('organisation', 'Organisation (optional)')}
          {f('email', 'Email')}
          {f('phone', 'Phone (E.164)', '+27.82…')}
          {f('street', 'Street')}
          {f('city', 'City')}
          {f('province', 'Province')}
          {f('postal_code', 'Postal code')}
          {f('country_code', 'Country code', 'ZA')}
        </div>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          <Button variant="primary" disabled={!c.first_name || !c.last_name || !c.email || !c.country_code || m.isPending} onClick={() => m.mutate()}>
            {m.isPending ? 'Saving…' : 'Save at registrar'}
          </Button>
        </div>
      </div>
    </Modal>
  );
}
