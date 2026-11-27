export const APP_NAME = 'Synkra Mail';
export const APP_VERSION = import.meta.env.VITE_APP_VERSION || 'dev';
export const DEFAULT_PAGE_SIZE = 50;
export const MESSAGE_BODY_CACHE_LIMIT = 200;
export const UNDO_SEND_DEFAULT_SECONDS = 10;
export const VAT_RATE_ZA = 0.15;
export const SA_PROVINCES = [
  'Eastern Cape', 'Free State', 'Gauteng', 'KwaZulu-Natal', 'Limpopo',
  'Mpumalanga', 'Northern Cape', 'North West', 'Western Cape',
] as const;

export const SUPPORTED_TIMEZONES = Intl.supportedValuesOf?.('timeZone') ?? ['Africa/Johannesburg', 'UTC'];

export const DNS_PROVIDER_HINTS: Record<string, { label: string; steps: (record: { type: string; name: string; value: string }) => string[] }> = {
  'xneelo-konsoleh': {
    label: 'xneelo (konsoleH)',
    steps: (r) => [
      'Log in to konsoleH → Manage Services → DNS',
      `Add a ${r.type} record where the host is "${r.name}"`,
      `Set the value to: ${r.value}`,
      'Save and allow up to 30 minutes for propagation',
    ],
  },
  'afrihost-clientzone': {
    label: 'Afrihost (ClientZone)',
    steps: (r) => [
      'Log in to ClientZone → My Products → DNS',
      `Create a ${r.type} record with name "${r.name}"`,
      `Value: ${r.value}`,
      'Save and re-check in Synkra Mail',
    ],
  },
  'domains-co-za': {
    label: 'Domains.co.za',
    steps: (r) => [
      'Log in to the Domains.co.za control panel → DNS Management',
      `Add ${r.type} — Name: "${r.name}", Value: "${r.value}"`,
      'Save. Propagation may take up to an hour',
    ],
  },
  'truehost': {
    label: 'Truehost',
    steps: (r) => [
      'Log in to Truehost cPanel → Zone Editor',
      `Add a ${r.type} record for "${r.name}"`,
      `Value: ${r.value}`,
      'Save',
    ],
  },
  'cloudflare': {
    label: 'Cloudflare',
    steps: (r) => [
      'Open the domain in Cloudflare → DNS → Records',
      `Add ${r.type} — Name: "${r.name}", Content: "${r.value}"`,
      'Set Proxy to DNS only for mail records',
      'Save',
    ],
  },
  'generic': {
    label: 'Any DNS provider',
    steps: (r) => [
      'Open your DNS management panel',
      `Add a ${r.type} record where the host is "${r.name}"`,
      `Set the value to: ${r.value}`,
      'Save and re-check',
    ],
  },
};
