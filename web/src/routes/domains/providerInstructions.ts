// providerInstructions.ts — per-record, per-provider setup steps for the DNS
// doctor. These are the four local providers South African businesses
// actually use (plus Cloudflare), each with the exact navigation path in
// their admin console. Steps reference the record's name/value verbatim.
export interface ProviderInstruction {
  provider: string;
  portal: string;
  steps: string[];
}

const PROVIDERS: { provider: string; portal: string }[] = [
  { provider: 'konsoleH', portal: 'konsoleh.hetzner.co.za' },
  { provider: 'ClientZone', portal: 'clientzone.afrihost.com' },
  { provider: 'Domains.co.za', portal: 'domains.co.za' },
  { provider: 'Truehost', portal: 'truehost.co.za' },
  { provider: 'Cloudflare', portal: 'dash.cloudflare.com' },
];

function stepsFor(provider: string, record: { type: string; name: string; value: string }): string[] {
  const where = record.name === '@' ? 'the root record (@)' : `a record with name "${record.name}"`;
  switch (provider) {
    case 'konsoleH':
      return [
        'Log in at konsoleh.hetzner.co.za and open the domain.',
        'Go to "Services" → "DNS" (zone editing).',
        `Add a ${record.type} record: set ${where}.`,
        `Paste the value: ${record.value}`,
        'Save — TTL 3600 is fine; lower to 300 while testing.',
      ];
    case 'ClientZone':
      return [
        'Log in at clientzone.afrihost.com.',
        'Open the domain under "Products & Services".',
        'Choose "DNS" → "Manage DNS records".',
        `Add a ${record.type} record at ${where}.`,
        `Paste the value: ${record.value}`,
        'Save. If a Cloudflare proxy toggle appears for this record, leave it DNS-only (grey cloud).',
      ];
    case 'Domains.co.za':
      return [
        'Log in at domains.co.za.',
        'Open "My Domains" → the domain → "DNS Management".',
        `Add a ${record.type} record ${where}.`,
        `Paste the value: ${record.value}`,
        'Save — the zone republishes within a minute.',
      ];
    case 'Truehost':
      return [
        'Log in at client.truehost.co.za.',
        'Open "Domains" → the domain → "DNS / Zone Editor".',
        `Add a ${record.type} record ${where}.`,
        `Paste the value: ${record.value}`,
        'Save and allow a few minutes for propagation.',
      ];
    case 'Cloudflare':
      return [
        'Open dash.cloudflare.com → the domain → "DNS" → "Records".',
        `Add a ${record.type} record: name ${record.name === '@' ? '@' : record.name}.`,
        `Paste the value: ${record.value}`,
        'Keep the proxy status DNS-only (grey cloud) — mail records must not be proxied.',
        'Save; propagation is usually under a minute.',
      ];
    default:
      return [`Add a ${record.type} record ${where} with value ${record.value}.`];
  }
}

export function providerInstructions(record: { type: string; name: string; value: string }): ProviderInstruction[] {
  return PROVIDERS.map((p) => ({ provider: p.provider, portal: p.portal, steps: stepsFor(p.provider, record) }));
}
