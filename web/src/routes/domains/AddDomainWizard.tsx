// AddDomainWizard — register a domain through the Oner reseller API:
//   1. availability check (ZARC via Oner)
//   2. registrant contact (the WHOIS record)
//   3. delegation choice — our authoritative nameservers (zone pre-provisioned)
//      or the customer's own (partially managed)
//   4. confirm → ZARC pending_create state
import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useMutation } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import { Button, Field, Input, Modal } from '@/design';
import type { RegistrantContact } from '@/lib/types';

const EMPTY: RegistrantContact = {
  first_name: '', last_name: '', email: '', phone: '', street: '', city: '',
  province: '', postal_code: '', country_code: 'ZA',
};

type CheckResult = { name: string; available: boolean; price?: number };

export function AddDomainWizard({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate();
  const [step, setStep] = useState<'search' | 'registrant' | 'delegation' | 'confirm'>('search');
  const [name, setName] = useState('');
  const [checks, setChecks] = useState<CheckResult[] | null>(null);
  const [registrant, setRegistrant] = useState<RegistrantContact>({ ...EMPTY });
  const [nsMode, setNsMode] = useState<'synkra' | 'custom'>('synkra');
  const [ns1, setNs1] = useState('');
  const [ns2, setNs2] = useState('');
  const [registeredId, setRegisteredId] = useState<string | null>(null);
  const [zoneNote, setZoneNote] = useState('');

  const checkM = useMutation({
    mutationFn: () => api.checkDomain([name.trim().toLowerCase()]),
    onSuccess: (results) => { setChecks(results); },
    onError: (e) => toast('error', 'Availability check failed', errorMessage(e)),
  });

  const registerM = useMutation({
    mutationFn: () => api.registerDomain({
      name: name.trim().toLowerCase(),
      years: 1,
      ns_mode: nsMode,
      custom_nameservers: nsMode === 'custom' ? [ns1, ns2] : undefined,
      registrant,
    }),
    onSuccess: (r) => {
      setRegisteredId(r.domain_id);
      setZoneNote(r.zone_provisioned
        ? 'The authoritative zone was provisioned with the full mail record set — ready before delegation.'
        : `Zone provisioning deferred: ${r.zone_error || 'you can provision it from the dashboard'}`);
      setStep('confirm');
    },
    onError: (e) => toast('error', 'Registration failed', errorMessage(e)),
  });

  const chosen = checks?.find((c) => c.available);

  return (
    <Modal open onClose={onClose} title="Register a domain" size="lg">
      <div className="space-y-4">
        {/* Step 1 — search */}
        {step === 'search' && (
          <div className="space-y-3">
            <Field label="Domain name">
              <Input
                value={name}
                onChange={(e) => { setName(e.target.value); setChecks(null); }}
                placeholder="yourbusiness.co.za"
              />
            </Field>
            <div className="flex items-center gap-2">
              <Button variant="primary" disabled={!name.includes('.')} onClick={() => checkM.mutate()}>
                {checkM.isPending ? 'Checking…' : 'Check availability'}
              </Button>
              {checks?.map((c) => (
                <span key={c.name} className={`text-sm px-2 py-1 rounded ${c.available ? 'bg-success/10 text-success' : 'bg-danger/10 text-danger'}`}>
                  {c.available ? `Available — R${c.price ?? 99}/yr` : 'Taken'}
                </span>
              ))}
            </div>
            {chosen && (
              <Button variant="primary" onClick={() => setStep('registrant')}>
                Continue with {chosen.name}
              </Button>
            )}
          </div>
        )}

        {/* Step 2 — registrant (WHOIS record) */}
        {step === 'registrant' && (
          <div className="space-y-3">
            <p className="text-sm text-ink-soft">The registrant is the legal domain holder — published to the registry (WHOIS privacy can mask it later).</p>
            <div className="grid grid-cols-2 gap-3">
              <Field label="First name"><Input value={registrant.first_name} onChange={(e) => setRegistrant({ ...registrant, first_name: e.target.value })} /></Field>
              <Field label="Last name"><Input value={registrant.last_name} onChange={(e) => setRegistrant({ ...registrant, last_name: e.target.value })} /></Field>
              <Field label="Email"><Input value={registrant.email} onChange={(e) => setRegistrant({ ...registrant, email: e.target.value })} /></Field>
              <Field label="Phone (E.164)"><Input value={registrant.phone} onChange={(e) => setRegistrant({ ...registrant, phone: e.target.value })} placeholder="+27.821234567" /></Field>
              <Field label="Street"><Input value={registrant.street} onChange={(e) => setRegistrant({ ...registrant, street: e.target.value })} /></Field>
              <Field label="City"><Input value={registrant.city} onChange={(e) => setRegistrant({ ...registrant, city: e.target.value })} /></Field>
              <Field label="Postal code"><Input value={registrant.postal_code} onChange={(e) => setRegistrant({ ...registrant, postal_code: e.target.value })} /></Field>
              <Field label="Country code"><Input value={registrant.country_code} onChange={(e) => setRegistrant({ ...registrant, country_code: e.target.value })} /></Field>
            </div>
            <div className="flex justify-between">
              <Button variant="ghost" onClick={() => setStep('search')}>Back</Button>
              <Button
                variant="primary"
                disabled={!registrant.first_name || !registrant.last_name || !registrant.email || registrant.country_code.length !== 2}
                onClick={() => setStep('delegation')}
              >
                Continue
              </Button>
            </div>
          </div>
        )}

        {/* Step 3 — delegation */}
        {step === 'delegation' && (
          <div className="space-y-3">
            <label className="flex items-start gap-3 p-3 border border-line rounded cursor-pointer">
              <input type="radio" checked={nsMode === 'synkra'} onChange={() => setNsMode('synkra')} className="mt-1" />
              <span>
                <span className="font-medium block">Use Synkra's nameservers (recommended)</span>
                <span className="text-xs text-ink-soft">We create the authoritative zone with MX, SPF, DKIM and DMARC before you switch — mail-ready DNS from day one.</span>
              </span>
            </label>
            <label className="flex items-start gap-3 p-3 border border-line rounded cursor-pointer">
              <input type="radio" checked={nsMode === 'custom'} onChange={() => setNsMode('custom')} className="mt-1" />
              <span>
                <span className="font-medium block">Keep my own nameservers</span>
                <span className="text-xs text-ink-soft">Partially managed: the domain registers through us, but DNS stays at your provider — publish the mail records from the doctor.</span>
              </span>
            </label>
            {nsMode === 'custom' && (
              <div className="grid grid-cols-2 gap-3">
                <Field label="Nameserver 1"><Input value={ns1} onChange={(e) => setNs1(e.target.value)} /></Field>
                <Field label="Nameserver 2"><Input value={ns2} onChange={(e) => setNs2(e.target.value)} /></Field>
              </div>
            )}
            <div className="flex justify-between">
              <Button variant="ghost" onClick={() => setStep('registrant')}>Back</Button>
              <Button
                variant="primary"
                disabled={nsMode === 'custom' && (!ns1 || !ns2)}
                onClick={() => setStep('confirm')}
              >
                Review
              </Button>
            </div>
          </div>
        )}

        {/* Step 4 — confirm */}
        {step === 'confirm' && !registeredId && (
          <div className="space-y-3">
            <div className="border border-line rounded p-3 text-sm space-y-1">
              <p><span className="text-ink-soft">Domain:</span> <span className="font-medium">{name}</span></p>
              <p><span className="text-ink-soft">Term:</span> 1 year · R{(checks?.find((c) => c.available)?.price ?? 99).toFixed(2)} (excl. VAT)</p>
              <p><span className="text-ink-soft">Registrant:</span> {registrant.first_name} {registrant.last_name} &lt;{registrant.email}&gt;</p>
              <p><span className="text-ink-soft">DNS:</span> {nsMode === 'synkra' ? "Synkra's authoritative nameservers (zone pre-provisioned)" : `Custom — ${ns1}, ${ns2}`}</p>
            </div>
            <div className="flex justify-between">
              <Button variant="ghost" onClick={() => setStep('delegation')}>Back</Button>
              <Button variant="primary" onClick={() => registerM.mutate()}>
                {registerM.isPending ? 'Registering…' : 'Register'}
              </Button>
            </div>
          </div>
        )}

        {/* Step 4 — result */}
        {step === 'confirm' && registeredId && (
          <div className="space-y-3">
            <p className="text-sm text-success font-medium">Registration submitted to the registry (ZARC).</p>
            <p className="text-sm text-ink-soft">{zoneNote}</p>
            <p className="text-sm text-ink-soft">Status: <span className="font-medium text-ink">pending_create</span> — the dashboard updates once the registry confirms.</p>
            <div className="flex justify-end gap-2">
              <Button variant="ghost" onClick={onClose}>Close</Button>
              <Button variant="primary" onClick={() => navigate(`/settings/domains/${registeredId}`)}>Open the dashboard</Button>
            </div>
          </div>
        )}
      </div>
    </Modal>
  );
}
