import { useRef } from 'react';
import clsx from 'clsx';
import { Icon } from '@/design/icons';
import { Button, Spinner } from '@/design';
import { formatBytes } from '@/lib/format';
import { useComposeStore } from './useComposeStore';
import type { StagedAttachment } from './types';

interface Props {
  onPick: (files: File[]) => void;
}

export function AttachmentBar({ onPick }: Props) {
  const attachments = useComposeStore((s) => s.attachments);
  const removeAttachment = useComposeStore((s) => s.removeAttachment);
  const uploading = useComposeStore((s) => s.uploading);
  const fileInputRef = useRef<HTMLInputElement>(null);

  if (attachments.length === 0 && uploading === 0) {
    return (
      <>
        <input
          ref={fileInputRef}
          type="file"
          multiple
          hidden
          onChange={(e) => {
            if (e.target.files) onPick(Array.from(e.target.files));
            e.target.value = '';
          }}
        />
      </>
    );
  }

  return (
    <div className="border-t border-line px-3 py-2">
      <div className="flex flex-wrap gap-2 items-center">
        {attachments.map((a) => (
          <AttachmentPill key={a.id} attachment={a} onRemove={() => removeAttachment(a.id)} />
        ))}
        {uploading > 0 && (
          <div className="inline-flex items-center gap-2 px-2 py-1 text-xs text-ink-soft border border-line rounded">
            <Spinner size={12} /> Uploading {uploading} file{uploading === 1 ? '' : 's'}…
          </div>
        )}
        <Button
          size="sm"
          variant="ghost"
          onClick={() => fileInputRef.current?.click()}
          aria-label="Add attachment"
        >
          <Icon name="attachment" size={12} /> Add
        </Button>
      </div>
      <input
        ref={fileInputRef}
        type="file"
        multiple
        hidden
        onChange={(e) => {
          if (e.target.files) onPick(Array.from(e.target.files));
          e.target.value = '';
        }}
      />
    </div>
  );
}

function AttachmentPill({ attachment, onRemove }: { attachment: StagedAttachment; onRemove: () => void }) {
  const errored = !!attachment.error;
  const uploading = !!attachment.uploading;
  return (
    <div
      className={clsx(
        'inline-flex items-center gap-2 px-2 py-1 text-xs border rounded max-w-[260px]',
        errored ? 'border-danger bg-danger-soft text-danger' : 'border-line bg-canvas',
      )}
    >
      {uploading ? <Spinner size={10} /> : <Icon name="attachment" size={10} />}
      <span className="truncate">{attachment.filename}</span>
      {!uploading && !errored && <span className="text-ink-mute">{formatBytes(attachment.size)}</span>}
      {errored && <span className="text-[10px]">Failed</span>}
      <button
        type="button"
        onClick={onRemove}
        className="text-ink-mute hover:text-ink"
        aria-label={`Remove attachment ${attachment.filename}`}
      >
        <Icon name="close" size={10} />
      </button>
    </div>
  );
}
