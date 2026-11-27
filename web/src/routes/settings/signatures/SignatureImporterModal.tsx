import { useState } from 'react';
import { api } from '@/lib/api';
import { Button, Modal, Textarea } from '@/design';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import { Icon } from '@/design/icons';
import type { SignatureImportResult } from '@/lib/types';

interface Props {
  open: boolean;
  onClose: () => void;
  onImported: (result: SignatureImportResult) => void;
}

export function SignatureImporterModal({ open, onClose, onImported }: Props) {
  const [html, setHtml] = useState('');
  const [result, setResult] = useState<SignatureImportResult | null>(null);
  const [busy, setBusy] = useState(false);

  async function analyse() {
    if (!html.trim()) return;
    setBusy(true);
    try {
      const res = await api.importSignatureHtml(html);
      setResult(res);
    } catch (e) {
      toast('error', 'Could not analyse HTML', errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  function use() {
    if (result) {
      onImported(result);
      onClose();
      setHtml('');
      setResult(null);
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Import HTML signature"
      description="Paste raw HTML from your existing signature. We will sanitise it, strip unsafe elements and warn about anything that will not render everywhere."
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>Cancel</Button>
          {result ? (
            <Button variant="primary" onClick={use}>Use this signature</Button>
          ) : (
            <Button variant="primary" onClick={analyse} loading={busy} disabled={!html.trim()}>
              Analyse HTML
            </Button>
          )}
        </>
      }
    >
      <div className="space-y-3">
        <Textarea
          value={html}
          onChange={(e) => setHtml(e.target.value)}
          rows={10}
          placeholder="<table>…</table>"
          className="font-mono text-xs"
        />
        {result && (
          <div className="space-y-3">
            <div className="border border-line rounded p-3">
              <p className="text-xs uppercase text-ink-mute mb-2">Sanitised HTML</p>
              <pre className="text-xs overflow-x-auto whitespace-pre-wrap break-all max-h-40 overflow-y-auto scroll-thin">{result.body_html}</pre>
            </div>
            {result.issues.length > 0 && (
              <div className="border border-line rounded">
                <p className="text-xs uppercase text-ink-mute px-3 py-2 border-b border-line">Compatibility issues</p>
                <ul className="max-h-48 overflow-y-auto scroll-thin text-xs">
                  {result.issues.map((issue, i) => (
                    <li key={i} className="px-3 py-2 border-t border-line first:border-t-0 flex items-start gap-2">
                      <Icon
                        name={issue.severity === 'error' ? 'x-circle' : issue.severity === 'warning' ? 'alert-triangle' : 'info'}
                        size={12}
                        className={`mt-0.5 shrink-0 ${issue.severity === 'error' ? 'text-danger' : issue.severity === 'warning' ? 'text-warning' : 'text-ink-mute'}`}
                      />
                      <div>
                        <p className="font-medium">{issue.message}</p>
                        {issue.fix && <p className="text-ink-soft mt-0.5">{issue.fix}</p>}
                      </div>
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        )}
      </div>
    </Modal>
  );
}
