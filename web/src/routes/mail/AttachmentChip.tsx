import { useState } from 'react';
import clsx from 'clsx';
import { Icon } from '@/design/icons';
import { Spinner } from '@/design';
import { api } from '@/lib/api';
import { formatBytes } from '@/lib/format';
import { toast } from '@/stores/toast';
import { errorMessage } from '@/lib/errors';
import type { Attachment } from '@/lib/types';

export function AttachmentChip({
  attachment,
  messageId,
}: {
  attachment: Attachment;
  messageId: string;
}) {
  const [busy, setBusy] = useState(false);

  async function download() {
    if (busy) return;
    setBusy(true);
    try {
      const blob = await api.fetchAttachmentBlob(messageId, attachment.id);
      triggerDownload(blob, attachment.filename);
    } catch (err) {
      toast('error', 'Could not download attachment', errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  const infected = attachment.scan_status === 'infected';
  const scanning = attachment.scan_status === 'pending';

  return (
    <button
      type="button"
      onClick={download}
      disabled={busy || infected || scanning}
      title={infected ? 'Blocked: this attachment was flagged by our scanner' : scanning ? 'Scanning…' : undefined}
      className={clsx(
        'inline-flex items-center gap-2 px-2 py-1 rounded border text-xs max-w-[260px] truncate',
        infected
          ? 'border-danger bg-danger-soft text-danger cursor-not-allowed'
          : scanning
          ? 'border-line bg-canvas text-ink-mute cursor-not-allowed'
          : 'border-line hover:bg-canvas',
      )}
    >
      {busy || scanning ? (
        <Spinner size={12} />
      ) : infected ? (
        <Icon name="x-circle" size={12} />
      ) : (
        <Icon name="attachment" size={12} />
      )}
      <span className="truncate">{attachment.filename}</span>
      <span className="text-ink-mute shrink-0">{formatBytes(attachment.size)}</span>
    </button>
  );
}

function triggerDownload(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 30_000);
}
