import { useState } from 'react';
import { Button } from '@/design';
import { Icon } from '@/design/icons';
import { toast } from '@/stores/toast';

export function CopyButton({ value, label, small }: { value: string; label?: string; small?: boolean }) {
  const [copied, setCopied] = useState(false);
  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      toast('success', 'Copied');
      setTimeout(() => setCopied(false), 1500);
    } catch {
      toast('error', 'Could not copy to clipboard');
    }
  }
  return (
    <Button size={small ? 'sm' : 'md'} variant="secondary" onClick={copy}>
      <Icon name={copied ? 'check' : 'info'} size={14} />
      {label ?? (copied ? 'Copied' : 'Copy')}
    </Button>
  );
}
