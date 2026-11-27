import { Badge } from '@/design';
import { Icon } from '@/design/icons';

export function AuthPanel({
  domain,
  auth,
}: {
  domain: string;
  auth: { spf: boolean; dkim: boolean; dmarc: boolean; mta_sts: boolean; tls_rpt: boolean; bimi: boolean };
}) {
  const items = [
    { key: 'SPF', value: auth.spf, description: 'Authorises sending servers.' },
    { key: 'DKIM', value: auth.dkim, description: 'Cryptographically signs messages.' },
    { key: 'DMARC', value: auth.dmarc, description: 'Tells receivers what to do when SPF/DKIM fail.' },
    { key: 'MTA-STS', value: auth.mta_sts, description: 'Enforces TLS when others send to you.' },
    { key: 'TLS-RPT', value: auth.tls_rpt, description: 'Reports about TLS failures.' },
    { key: 'BIMI', value: auth.bimi, description: 'Displays your logo in supporting clients.' },
  ];
  return (
    <div className="grid grid-cols-2 md:grid-cols-3 gap-2">
      {items.map((it) => (
        <div key={it.key} className={`p-3 rounded border ${it.value ? 'border-success/40 bg-success-soft' : 'border-line bg-surface'}`}>
          <div className="flex items-center gap-2">
            <Icon
              name={it.value ? 'check-circle' : 'minus'}
              size={14}
              className={it.value ? 'text-success' : 'text-ink-mute'}
            />
            <p className="text-sm font-medium">{it.key}</p>
            <Badge tone={it.value ? 'success' : 'neutral'}>{it.value ? 'on' : 'off'}</Badge>
          </div>
          <p className="text-xs text-ink-soft mt-1">{it.description}</p>
        </div>
      ))}
      <div className="p-3 rounded border border-line bg-surface col-span-2 md:col-span-3">
        <p className="text-xs text-ink-mute">
          Records for <strong>{domain}</strong> are shown in the DNS doctor for this domain.
        </p>
      </div>
    </div>
  );
}
