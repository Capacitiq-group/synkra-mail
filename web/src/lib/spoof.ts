// Client-side spoof & phishing heuristics for the reading pane (catalog part
// 2: "spoofing & security alerts"). Advisory signals — never a verdict. The
// authoritative signal is the auth_results map the ingest pipeline stores.
import type { MessageSummary } from './types'

export type Severity = 'high' | 'medium' | 'low'

export interface SpoofAlert {
  severity: Severity
  title: string
  detail: string
}

const BRAND_DOMAINS: { brand: string; domains: string[] }[] = [
  { brand: 'Google', domains: ['google.com', 'accounts.google.com'] },
  { brand: 'Microsoft', domains: ['microsoft.com', 'outlook.com', 'live.com', 'office365.com'] },
  { brand: 'SARS', domains: ['sars.gov.za'] },
  { brand: 'FNB', domains: ['fnb.co.za'] },
  { brand: 'Standard Bank', domains: ['standardbank.co.za'] },
  { brand: 'Absa', domains: ['absa.co.za'] },
  { brand: 'Nedbank', domains: ['nedbank.co.za'] },
  { brand: 'DHL', domains: ['dhl.com'] },
]

/** NFKD skeleton: fold confusables/accents so homoglyph tricks collapse. */
function skeleton(s: string): string {
  return s
    .normalize('NFKD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/-/g, '')
    .replace(/0/g, 'o')
    .replace(/1|l/g, 'l')
    .replace(/3/g, 'e')
    .replace(/5/g, 's')
    .replace(/7/g, 't')
    .replace(/4/g, 'a')
    .replace(/rn/g, 'm')
    .replace(/vv/g, 'w')
}

/** First label of the registrable domain: 'fnb-secure' from 'fnb-secure.co.za'.
 *  Handles two-level public suffixes (co.za, org.uk, com.au, …). */
const SECOND_LEVEL_SUFFIXES = ['co', 'org', 'net', 'gov', 'ac', 'com']
function firstLabel(domain: string): string {
  const parts = domain.split('.')
  if (parts.length >= 3) {
    const penultimate = parts[parts.length - 2]
    const tld = parts[parts.length - 1]
    if (SECOND_LEVEL_SUFFIXES.includes(penultimate) && tld.length === 2) {
      return parts[parts.length - 3]
    }
  }
  return parts.length >= 2 ? parts[parts.length - 2] : domain
}

function domainOf(email: string): string {
  return (email.split('@')[1] ?? '').toLowerCase()
}

export function analyzeMessage(m: MessageSummary): SpoofAlert[] {
  const alerts: SpoofAlert[] = []

  // 1. Authentication failures (authoritative, from the ingest pipeline)
  const ar = m.auth_results
  if (ar && (ar.dmarc === 'fail' || (ar.spf === 'fail' && ar.dkim === 'fail'))) {
    alerts.push({
      severity: 'high',
      title: 'Sender authentication failed',
      detail: `SPF: ${ar.spf}, DKIM: ${ar.dkim}, DMARC: ${ar.dmarc}. The message did not pass the sender domain's own authentication — treat links and attachments as hostile.`,
    })
  } else if (ar && (ar.spf === 'fail' || ar.dkim === 'fail')) {
    alerts.push({
      severity: 'medium',
      title: 'Partial authentication failure',
      detail: `SPF: ${ar.spf}, DKIM: ${ar.dkim}, DMARC: ${ar.dmarc}. Verify the sender before acting on this message.`,
    })
  }

  // 2. Display name claims an address that differs from the real sender
  const displayName = m.from.name ?? ''
  const claimed = displayName.match(/[\w.+-]+@[\w.-]+\.\w+/)
  if (claimed && claimed[0].toLowerCase() !== m.from.email.toLowerCase()) {
    alerts.push({
      severity: 'high',
      title: 'Sender name does not match the address',
      detail: `The display name claims "${claimed[0]}" but the message actually comes from ${m.from.email}.`,
    })
  }

  // 3. Brand lookalikes: compare collapsed registrable labels
  // ('fnb-secure' ≈ 'fnb') so typosquats and dash-sandwiches both trip.
  const actual = domainOf(m.from.email)
  const actualBrand = skeleton(firstLabel(actual))
  for (const b of BRAND_DOMAINS) {
    if (b.domains.includes(actual)) break
    for (const d of b.domains) {
      const brandLabel = skeleton(firstLabel(d))
      const lookalike =
        (actualBrand !== '' &&
          actualBrand !== brandLabel &&
          (actualBrand.includes(brandLabel) || brandLabel.includes(actualBrand))) ||
        (actual !== '' && actual !== d && skeleton(actual) === skeleton(d))
      if (lookalike) {
        alerts.push({
          severity: 'high',
          title: 'Lookalike sender domain',
          detail: `"${actual}" closely mimics "${d}" (typical of phishing). ${b.brand} will never send from a lookalike domain.`,
        })
        break
      }
    }
  }

  // 4. High spam score (backend classifier)
  if ((m.spam_score ?? 0) >= 8) {
    alerts.push({
      severity: 'high',
      title: 'High spam score',
      detail: `The spam classifier scored this message ${(m.spam_score ?? 0).toFixed(1)} / 10.`,
    })
  } else if ((m.spam_score ?? 0) >= 5) {
    alerts.push({
      severity: 'medium',
      title: 'Elevated spam score',
      detail: `The spam classifier scored this message ${(m.spam_score ?? 0).toFixed(1)} / 10.`,
    })
  }

  return alerts
}
