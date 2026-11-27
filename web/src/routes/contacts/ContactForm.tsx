import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useContactGroups, useCreateContact, useUpdateContact } from '@/hooks/queries/contacts';
import { Button, Checkbox, Field, Input, Textarea } from '@/design';
import { Icon } from '@/design/icons';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import { isEmail } from '@/lib/validators';
import type { Address, Contact } from '@/lib/types';

interface Props {
  contact?: Contact;
}

interface EmailRow { email: string; label: string }
interface PhoneRow { number: string; label: string }


export function ContactForm({ contact }: Props) {
  const navigate = useNavigate();
  const create = useCreateContact();
  const update = useUpdateContact();
  const groupsQ = useContactGroups();

  const [displayName, setDisplayName] = useState(contact?.display_name ?? '');
  const [emails, setEmails] = useState<EmailRow[]>(
    (contact?.emails ?? [{ email: '', name: undefined }]).map((e) => ({ email: e.email, label: 'work' })),
  );
  const [phones, setPhones] = useState<PhoneRow[]>(
    (contact?.phones ?? []).map((p) => ({ number: p, label: 'mobile' })),
  );
  const [company, setCompany] = useState(contact?.company ?? '');
  const [jobTitle, setJobTitle] = useState(contact?.job_title ?? '');
  const [notes, setNotes] = useState(contact?.notes ?? '');
  const [groups, setGroups] = useState<string[]>(contact?.groups ?? []);
  const [orgVisible, setOrgVisible] = useState(contact?.org_visible ?? true);
  const [err, setErr] = useState<string | null>(null);

  function addEmail() { setEmails([...emails, { email: '', label: 'work' }]); }
  function removeEmail(i: number) { setEmails(emails.filter((_, idx) => idx !== i)); }
  function updateEmail(i: number, patch: Partial<EmailRow>) {
    setEmails(emails.map((e, idx) => (idx === i ? { ...e, ...patch } : e)));
  }
  function addPhone() { setPhones([...phones, { number: '', label: 'mobile' }]); }
  function removePhone(i: number) { setPhones(phones.filter((_, idx) => idx !== i)); }
  function updatePhone(i: number, patch: Partial<PhoneRow>) {
    setPhones(phones.map((p, idx) => (idx === i ? { ...p, ...patch } : p)));
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setErr(null);
    const validEmails = emails.filter((x) => x.email.trim());
    if (!displayName.trim()) { setErr('Name is required'); return; }
    if (validEmails.length === 0) { setErr('At least one email is required'); return; }
    if (!validEmails.every((x) => isEmail(x.email))) { setErr('One or more email addresses are invalid'); return; }

    const addresses: Address[] = validEmails.map((x) => ({ email: x.email.trim(), name: displayName.trim() }));
    const payload = {
      display_name: displayName.trim(),
      emails: addresses,
      phones: phones.map((p) => p.number.trim()).filter(Boolean),
      company: company.trim() || undefined,
      job_title: jobTitle.trim() || undefined,
      notes: notes.trim() || undefined,
      groups,
      org_visible: orgVisible,
      avatar_url: contact?.avatar_url,
    };

    try {
      if (contact) {
        await update.mutateAsync({ id: contact.id, patch: payload });
        toast('success', 'Contact updated');
      } else {
        await create.mutateAsync(payload);
        toast('success', 'Contact created');
      }
      navigate('/contacts');
    } catch (e2) {
      setErr(errorMessage(e2));
    }
  }

  return (
    <form onSubmit={submit} className="p-4 md:p-6 max-w-2xl mx-auto space-y-4">
      <Field label="Name" required>
        <Input value={displayName} onChange={(e) => setDisplayName(e.target.value)} autoFocus />
      </Field>

      <div>
        <p className="text-xs uppercase text-ink-mute mb-2">Email addresses</p>
        <div className="space-y-2">
          {emails.map((e, i) => (
            <div key={i} className="flex gap-1">
              <Input
                type="email"
                value={e.email}
                onChange={(x) => updateEmail(i, { email: x.target.value })}
                placeholder="name@example.com"
                invalid={!!e.email && !isEmail(e.email)}
              />
              <Input
                value={e.label}
                onChange={(x) => updateEmail(i, { label: x.target.value })}
                placeholder="label"
                className="max-w-[100px]"
              />
              {emails.length > 1 && (
                <button
                  type="button"
                  onClick={() => removeEmail(i)}
                  className="p-2 text-ink-mute hover:text-danger"
                  aria-label="Remove email"
                ><Icon name="close" size={14} /></button>
              )}
            </div>
          ))}
        </div>
        <Button type="button" size="sm" variant="ghost" onClick={addEmail} className="mt-1">
          <Icon name="plus" size={12} /> Add email
        </Button>
      </div>

      <div>
        <p className="text-xs uppercase text-ink-mute mb-2">Phone numbers</p>
        <div className="space-y-2">
          {phones.map((p, i) => (
            <div key={i} className="flex gap-1">
              <Input
                type="tel"
                value={p.number}
                onChange={(x) => updatePhone(i, { number: x.target.value })}
                placeholder="+27 …"
              />
              <Input
                value={p.label}
                onChange={(x) => updatePhone(i, { label: x.target.value })}
                placeholder="label"
                className="max-w-[100px]"
              />
              <button
                type="button"
                onClick={() => removePhone(i)}
                className="p-2 text-ink-mute hover:text-danger"
                aria-label="Remove phone"
              ><Icon name="close" size={14} /></button>
            </div>
          ))}
        </div>
        <Button type="button" size="sm" variant="ghost" onClick={addPhone} className="mt-1">
          <Icon name="plus" size={12} /> Add phone
        </Button>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <Field label="Company">
          <Input value={company} onChange={(e) => setCompany(e.target.value)} />
        </Field>
        <Field label="Job title">
          <Input value={jobTitle} onChange={(e) => setJobTitle(e.target.value)} />
        </Field>
      </div>

      <Field label="Notes">
        <Textarea value={notes} onChange={(e) => setNotes(e.target.value)} rows={3} />
      </Field>

      {(groupsQ.data?.length ?? 0) > 0 && (
        <Field label="Groups">
          <div className="flex flex-wrap gap-2">
            {groupsQ.data!.map((g) => {
              const checked = groups.includes(g.id);
              return (
                <label
                  key={g.id}
                  className={`px-2 py-1 text-xs rounded border cursor-pointer ${
                    checked ? 'bg-accent-soft border-accent text-accent' : 'border-line'
                  }`}
                >
                  <input
                    type="checkbox"
                    className="sr-only"
                    checked={checked}
                    onChange={(e) =>
                      setGroups(e.target.checked ? [...groups, g.id] : groups.filter((x) => x !== g.id))
                    }
                  />
                  {g.name}
                </label>
              );
            })}
          </div>
        </Field>
      )}

      <Checkbox
        label="Visible to everyone in my organisation"
        checked={orgVisible}
        onChange={(e) => setOrgVisible(e.target.checked)}
      />

      {err && <p role="alert" className="text-sm text-danger">{err}</p>}

      <div className="flex gap-2 pt-3 border-t border-line">
        <Button type="submit" variant="primary" loading={create.isPending || update.isPending}>
          {contact ? 'Save changes' : 'Create contact'}
        </Button>
        <Button type="button" variant="secondary" onClick={() => navigate(-1 as never)}>Cancel</Button>
      </div>
    </form>
  );
}
