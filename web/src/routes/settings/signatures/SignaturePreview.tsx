import { useMemo, useState } from 'react';
import { useAuth } from '@/stores/auth';
import { Tabs } from '@/design';
import { lintSignatureHtml, hasBlockingIssues } from '@/lib/lintSignature';
import { renderEmail, wrapForIframe } from '@/lib/sanitize';
import { resolvePlaceholders, type PlaceholderContext } from '@/lib/resolvePlaceholders';
import { Icon } from '@/design/icons';

type ClientMode = 'gmail' | 'outlook' | 'apple';

const CLIENT_CSS: Record<ClientMode, string> = {
  gmail: "font-family: Arial, sans-serif; font-size: 13px;",
  outlook: "font-family: 'Segoe UI', Calibri, sans-serif; font-size: 12px;",
  apple: "font-family: -apple-system, BlinkMacSystemFont, sans-serif; font-size: 13px;",
};

interface Props {
  html: string;
  placeholderContext: PlaceholderContext;
  showLint?: boolean;
}

export function SignaturePreview({ html, placeholderContext, showLint = true }: Props) {
  const [client, setClient] = useState<ClientMode>('gmail');

  const resolved = useMemo(
    () => resolvePlaceholders(html, placeholderContext),
    [html, placeholderContext],
  );

  const lint = useMemo(() => lintSignatureHtml(resolved), [resolved]);
  const blocking = hasBlockingIssues(lint);

  const rendered = useMemo(
    () => renderEmail(resolved, { allowRemoteImages: true }),
    [resolved],
  );

  const srcDoc = useMemo(() => {
    const wrapper = `<div style="${CLIENT_CSS[client]}">${rendered.html}</div>`;
    const base = wrapForIframe(wrapper, { allowRemoteImages: true });
    return base.replace(
      '</style>',
      `</style><style>${clientOverride(client)}</style>`,
    );
  }, [rendered.html, client]);

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-2">
        <Tabs
          items={[
            { id: 'gmail', label: 'Gmail' },
            { id: 'outlook', label: 'Outlook' },
            { id: 'apple', label: 'Apple Mail' },
          ]}
          value={client}
          onChange={(v) => setClient(v as ClientMode)}
        />
      </div>

      <div className="border border-line rounded overflow-hidden bg-white">
        <iframe
          title={`Signature preview (${client})`}
          sandbox="allow-popups"
          srcDoc={srcDoc}
          className="w-full border-0"
          style={{ height: 240 }}
        />
      </div>

      {showLint && lint.length > 0 && (
        <div className="border border-line rounded">
          <div className="px-3 py-2 border-b border-line bg-canvas text-xs font-medium flex items-center gap-2">
            <Icon name="info" size={12} />
            Compatibility ({lint.filter((i) => i.severity === 'error').length} error, {lint.filter((i) => i.severity === 'warning').length} warning)
          </div>
          <ul className="max-h-48 overflow-y-auto scroll-thin text-xs">
            {lint.map((issue, i) => (
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

      {showLint && blocking && (
        <div className="border border-danger/40 bg-danger-soft rounded p-3 text-sm text-danger">
          Fix the errors above before saving — many clients will render this signature incorrectly.
        </div>
      )}
    </div>
  );
}

function clientOverride(client: ClientMode): string {
  // Simulated narrowing that approximates each client's rendering quirks.
  // Not pixel-perfect; documented as "compatibility approximation" in the UI.
  if (client === 'outlook') {
    return '.email-body img { -ms-interpolation-mode: bicubic; } .email-body table { border-collapse: collapse; }';
  }
  if (client === 'gmail') {
    return '.email-body a { color: #0f766e !important; }';
  }
  return '';
}

// Suppress unused warning for useAuth imported above (used by callers).
void useAuth;
