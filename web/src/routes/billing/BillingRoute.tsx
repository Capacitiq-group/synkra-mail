// BillingRoute — the billing document (phase 11): plan summary, billing
// contact, invoice list with per-line 15% VAT, Paystack checkout, payment
// methods and payment history.
import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import { usePageTitle } from '@/hooks';
import { Button, Field, Input, Skeleton } from '@/design';
import { Panel } from '@/components/Banner';
import type { InvoiceView } from '@/lib/types';

export function BillingRoute() {
  usePageTitle('Billing');
  const qc = useQueryClient();
  const summaryQ = useQuery({ queryKey: ['billing'], queryFn: () => api.billingSummary() });
  const invoicesQ = useQuery({ queryKey: ['invoices'], queryFn: () => api.listInvoices() });
  const [openInvoice, setOpenInvoice] = useState<string | null>(null);
  const [contactOpen, setContactOpen] = useState(false);

  const pay = useMutation({
    mutationFn: (id: string) => api.payInvoice(id),
    onSuccess: (r) => {
      if (typeof window !== 'undefined') window.open(r.pay_url, '_blank');
      toast('info', 'Checkout opened', 'Complete the card payment; the invoice reconciles via verify.');
      qc.invalidateQueries({ queryKey: ['invoices'] });
    },
    onError: (e) => toast('error', 'Could not start checkout', errorMessage(e)),
  });
  const markPaid = useMutation({
    mutationFn: (id: string) => api.markInvoicePaid(id),
    onSuccess: () => { toast('success', 'Marked paid (manual capture)'); qc.invalidateQueries({ queryKey: ['invoices'] }); },
    onError: (e) => toast('error', 'Failed', errorMessage(e)),
  });
  const gen = useMutation({
    mutationFn: () => api.createInvoice(),
    onSuccess: () => { toast('success', 'Invoice generated'); qc.invalidateQueries({ queryKey: ['invoices'] }); },
    onError: (e) => toast('error', 'Could not generate', errorMessage(e)),
  });
  const removeMethod = useMutation({
    mutationFn: (id: string) => api.removePaymentMethod(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['billing'] }),
    onError: (e) => toast('error', 'Failed', errorMessage(e)),
  });

  if (summaryQ.isLoading) return <div className="p-6"><Skeleton className="h-64 w-full" /></div>;
  if (summaryQ.isError || !summaryQ.data) return <p className="p-6 text-sm text-danger">Could not load billing.</p>;
  const { plan, subscription, contact, payment_methods, paystack_configured, next_invoice_lines } = summaryQ.data;

  return (
    <div className="p-4 md:p-6 max-w-4xl mx-auto space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-semibold">Billing</h1>
          <p className="text-sm text-ink-soft">All amounts in ZAR. VAT is charged at 15% per line.</p>
        </div>
        <Button variant="primary" onClick={() => gen.mutate()} disabled={gen.isPending}>
          {gen.isPending ? 'Generating…' : 'Generate invoice'}
        </Button>
      </div>

      <Panel title="Plan">
        <div className="flex flex-wrap justify-between gap-3 text-sm">
          <div>
            <p className="font-medium">{plan.name}</p>
            <p className="text-ink-soft">{plan.mailbox_limit} mailboxes · {plan.storage_total_mb} MB storage included</p>
          </div>
          <div className="text-right">
            <p className="font-medium">R{(plan.price_cents / 100).toFixed(2)} <span className="text-ink-mute font-normal">/ mailbox / month</span></p>
            <p className="text-ink-soft">subscription: {subscription.status}</p>
          </div>
        </div>
        <div className="border-t border-line pt-2 text-sm text-ink-soft">
          Next invoice preview:
          <ul className="mt-1 space-y-0.5">
            {next_invoice_lines.map((l, i) => (
              <li key={i}>{l.description}{l.quantity > 1 ? ` × ${l.quantity}` : ''} — R{(l.unit_zar * l.quantity).toFixed(2)} ex VAT</li>
            ))}
          </ul>
        </div>
      </Panel>

      <Panel
        title="Billing contact"
        action={<Button variant="ghost" onClick={() => setContactOpen(!contactOpen)}>{contactOpen ? 'Close' : 'Edit'}</Button>}
      >
        {contactOpen ? (
          <ContactForm contact={contact} onSaved={() => setContactOpen(false)} />
        ) : (
          <p className="text-sm">
            {contact.billing_email || <span className="text-warning">No billing e-mail set</span>}
            {contact.billing_contact_name && ` · ${contact.billing_contact_name}`}
            {contact.vat_number && ` · VAT ${contact.vat_number}`}
          </p>
        )}
      </Panel>

      <Panel title="Invoices">
        {invoicesQ.data?.invoices.length === 0 && <p className="text-sm text-ink-soft">No invoices yet.</p>}
        <div className="space-y-2">
          {invoicesQ.data?.invoices.map((inv) => (
            <InvoiceRow
              key={inv.id}
              inv={inv}
              open={openInvoice === inv.id}
              onToggle={() => setOpenInvoice(openInvoice === inv.id ? null : inv.id)}
              paystackConfigured={paystack_configured}
              onPay={() => pay.mutate(inv.id)}
              onMarkPaid={() => markPaid.mutate(inv.id)}
            />
          ))}
        </div>
      </Panel>

      <Panel title="Payment methods">
        {payment_methods.length === 0 && <p className="text-sm text-ink-soft">No cards on file — one is saved after a successful Paystack checkout.</p>}
        <div className="space-y-1">
          {payment_methods.map((m) => (
            <div key={m.id} className="flex items-center justify-between text-sm">
              <span>{m.masked} {m.holder && `· ${m.holder}`} {m.exp_month && `· ${String(m.exp_month).padStart(2, '0')}/${m.exp_year}`}</span>
              <Button variant="ghost" onClick={() => removeMethod.mutate(m.id)}>Remove</Button>
            </div>
          ))}
        </div>
        {!paystack_configured && <p className="text-xs text-warning">Card charging is not configured on this deployment (SYNKRA_MAIL_PAYSTACK_SECRET_KEY unset).</p>}
      </Panel>

      <Panel title="Payment history">
        {invoicesQ.data?.payments.length === 0 && <p className="text-sm text-ink-soft">No payments yet.</p>}
        <div className="space-y-1">
          {invoicesQ.data?.payments.map((p) => (
            <div key={p.id} className="flex items-center justify-between text-sm">
              <span>{p.paid_at ? new Date(p.paid_at).toLocaleDateString('en-ZA') : new Date(p.created_at).toLocaleDateString('en-ZA')} · {p.provider}</span>
              <span>
                R{p.amount_zar.toFixed(2)}{' '}
                <span className={p.status === 'success' ? 'text-success' : p.status === 'pending' ? 'text-warning' : 'text-danger'}>
                  {p.status}
                </span>
              </span>
            </div>
          ))}
        </div>
      </Panel>
    </div>
  );
}

function InvoiceRow({ inv, open, onToggle, paystackConfigured, onPay, onMarkPaid }: {
  inv: InvoiceView; open: boolean; onToggle: () => void;
  paystackConfigured: boolean; onPay: () => void; onMarkPaid: () => void;
}) {
  return (
    <div className="border border-line rounded">
      <button type="button" onClick={onToggle} className="w-full text-left p-3 flex items-center justify-between gap-3">
        <div>
          <span className="font-medium">{inv.number}</span>
          <span className="text-ink-soft text-sm ml-2">{new Date(inv.issued_at).toLocaleDateString('en-ZA')}</span>
        </div>
        <div className="flex items-center gap-2">
          <span className="font-medium">R{inv.total_zar.toFixed(2)}</span>
          <span className={`text-xs px-2 py-0.5 rounded ${inv.status === 'paid' ? 'bg-success/10 text-success' : 'bg-warning/10 text-warning'}`}>{inv.status}</span>
        </div>
      </button>
      {open && (
        <div className="px-3 pb-3 space-y-2 text-sm">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-ink-mute">
                <th className="py-1">Description</th><th>Qty</th><th className="text-right">Unit</th>
                <th className="text-right">VAT 15%</th><th className="text-right">Total</th>
              </tr>
            </thead>
            <tbody>
              {inv.lines.map((l, i) => (
                <tr key={i} className="border-t border-line">
                  <td className="py-1 pr-2">{l.description}</td>
                  <td>{l.quantity}</td>
                  <td className="text-right">R{l.unit_zar.toFixed(2)}</td>
                  <td className="text-right">R{l.vat_zar.toFixed(2)}</td>
                  <td className="text-right font-medium">R{l.total_zar.toFixed(2)}</td>
                </tr>
              ))}
              <tr className="border-t border-line font-medium">
                <td colSpan={3} className="py-1">Subtotal · VAT · Total</td>
                <td className="text-right">R{inv.vat_zar.toFixed(2)}</td>
                <td className="text-right">R{inv.total_zar.toFixed(2)}</td>
              </tr>
            </tbody>
          </table>
          {inv.status === 'open' && (
            <div className="flex gap-2 justify-end">
              {paystackConfigured ? (
                <Button variant="primary" onClick={onPay}>Pay with Paystack</Button>
              ) : (
                <span className="text-xs text-warning self-center">Charging not configured on this deployment.</span>
              )}
              <Button variant="ghost" onClick={onMarkPaid}>Mark paid (manual)</Button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function ContactForm({ contact, onSaved }: {
  contact: { billing_email: string; billing_contact_name: string; vat_number: string }; onSaved: () => void;
}) {
  const [email, setEmail] = useState(contact.billing_email);
  const [name, setName] = useState(contact.billing_contact_name);
  const [vat, setVat] = useState(contact.vat_number);
  const m = useMutation({
    mutationFn: () => api.updateBillingContact({ billing_email: email, billing_contact_name: name, vat_number: vat }),
    onSuccess: () => { toast('success', 'Billing contact saved'); onSaved(); },
    onError: (e) => toast('error', 'Save failed', errorMessage(e)),
  });
  return (
    <div className="space-y-3">
      <Field label="Billing e-mail"><Input value={email} onChange={(e) => setEmail(e.target.value)} /></Field>
      <Field label="Contact name"><Input value={name} onChange={(e) => setName(e.target.value)} /></Field>
      <Field label="VAT number"><Input value={vat} onChange={(e) => setVat(e.target.value)} /></Field>
      <div className="flex justify-end">
        <Button variant="primary" disabled={!email || m.isPending} onClick={() => m.mutate()}>Save</Button>
      </div>
    </div>
  );
}
