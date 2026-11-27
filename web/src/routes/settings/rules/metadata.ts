import type { RuleAction, RuleCondition } from '@/lib/types';

export const CONDITION_LABELS: Record<RuleCondition['type'], { label: string; placeholder: string; description: string }> = {
  from_matches: { label: 'Sender matches', placeholder: 'name@example.com or *@example.com', description: 'Exact address, or *@domain for a whole domain.' },
  to_matches: { label: 'Recipient matches', placeholder: 'name@example.com or *@example.com', description: 'Matches the To field only — not Cc or Bcc.' },
  subject_contains: { label: 'Subject contains', placeholder: 'invoice', description: 'Case-insensitive substring match.' },
  body_contains: { label: 'Body contains', placeholder: 'payment due', description: 'Case-insensitive. Slower — matches on the decoded text body.' },
  has_attachment: { label: 'Has attachment', placeholder: '', description: 'Any message with at least one attachment.' },
  is_external: { label: 'Is external sender', placeholder: '', description: 'Sender domain differs from your organisation\'s domains.' },
  address_matches: { label: 'Sent to my address', placeholder: 'sales@company.co.za', description: 'Any of To, Cc or Bcc matches this address.' },
  alias_id: { label: 'Received via alias', placeholder: 'Select an alias', description: 'Matches messages sent to a specific alias.' },
};

export const ACTION_LABELS: Record<RuleAction['type'], { label: string; description: string }> = {
  forward: { label: 'Forward', description: 'Forward a copy to another address, keeping the original in your mailbox.' },
  redirect: { label: 'Redirect', description: 'Re-send the message so replies go to the new recipient — the original is not kept.' },
  apply_label: { label: 'Apply label', description: 'Add a coloured label to the message.' },
  move: { label: 'Move to folder', description: 'Move the message to a folder.' },
  mark_important: { label: 'Mark important', description: 'Flag the message as important.' },
  notify_colleague: { label: 'Notify colleague', description: 'Send a short notification to a colleague.' },
  create_task: { label: 'Create task', description: 'Add a task with a subject derived from the message.' },
  auto_reply: { label: 'Auto-reply', description: 'Reply automatically with a chosen template.' },
  add_to_shared_mailbox: { label: 'Add to shared mailbox', description: 'Place a copy into a shared mailbox for a team to action.' },
};

export function describeCondition(c: RuleCondition): string {
  const meta = CONDITION_LABELS[c.type];
  if (c.type === 'has_attachment' || c.type === 'is_external') {
    return `${meta.label}: ${c.value ? 'yes' : 'no'}`;
  }
  return `${meta.label}: ${c.value}`;
}

export function describeAction(a: RuleAction): string {
  const meta = ACTION_LABELS[a.type];
  switch (a.type) {
    case 'forward':
      return `${meta.label}: ${a.to.join(', ')}`;
    case 'redirect':
      return `${meta.label}: ${a.to}`;
    case 'notify_colleague':
      return `${meta.label}: ${a.email}`;
    case 'create_task':
    case 'apply_label':
    case 'move':
      return meta.label;
    default:
      return meta.label;
    case 'add_to_shared_mailbox':
    case 'auto_reply':
      return meta.label;
  }
}

export const NEW_CONDITION_DEFAULTS: Record<RuleCondition['type'], RuleCondition> = {
  from_matches: { type: 'from_matches', value: '' },
  to_matches: { type: 'to_matches', value: '' },
  subject_contains: { type: 'subject_contains', value: '' },
  body_contains: { type: 'body_contains', value: '' },
  has_attachment: { type: 'has_attachment', value: true },
  is_external: { type: 'is_external', value: true },
  address_matches: { type: 'address_matches', value: '' },
  alias_id: { type: 'alias_id', value: '' },
};

export const NEW_ACTION_DEFAULTS: Record<RuleAction['type'], RuleAction> = {
  forward: { type: 'forward', to: [] },
  redirect: { type: 'redirect', to: '' },
  apply_label: { type: 'apply_label', label_id: '' },
  move: { type: 'move', folder_id: '' },
  mark_important: { type: 'mark_important' },
  notify_colleague: { type: 'notify_colleague', email: '', message: '' },
  create_task: { type: 'create_task', list_id: '', title_template: '{{subject}}' },
  auto_reply: { type: 'auto_reply', template_id: '' },
  add_to_shared_mailbox: { type: 'add_to_shared_mailbox', shared_mailbox_id: '' },
};
