import type { MessageView } from '@/types/api'

/**
 * Client-side spoof & phishing heuristics (catalog Part 2: "spoofing &
 * security alerts"). These are ADVISORY signals rendered as banners in the
 * reading pane — never a verdict. The authoritative signals are the
 * SPF/DKIM/DMARC results the backend stores per message.
 */

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
function skeleton(domain: string): string {
  const folded = domain
    .normalize('NFKD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
  // common confusable substitutions seen in phishing kits
  return folded
    .replace(/0/g, 'o')
    .replace(/1|l/g, 'l')
    .replace(/3/g, 'e')
    .replace(/5/g, 's')
    .replace(/7/g, 't')
    .replace(/4/g, 'a')
    .replace(/rn/g, 'm')
    .replace(/vv/g, 'w')
}

function domainOf(email: string): string {
  return (email.split('@')[1] ?? '').toLowerCase()
}

export function authResultsStatus(authResults: Record<string, unknown> | null): {
  spf: string
  dkim: string
  dmarc: string
} {
  const pick = (k: string): string => {
    const v = authResults?.[k]
    if (typeof v === 'string') return v.toLowerCase()
    if (v && typeof v === 'object') {
      const rec = v as Record<string, unknown>
      const r = rec.result ?? rec.status ?? rec.value
      if (typeof r === 'string') return r.toLowerCase()
    }
    return 'unknown'
  }
  return { spf: pick('spf'), dkim: pick('dkim'), dmarc: pick('dmarc') }
}

export function analyzeMessage(m: MessageView): SpoofAlert[] {
  const alerts: SpoofAlert[] = []

  // 1. Authentication results (authoritative, from the ingest pipeline)
  const ar = authResultsStatus(m.auth_results)
  if (ar.dmarc === 'fail') {
    alerts.push({
      severity: 'high',
      title: 'DMARC failed',
      detail: `This message did not pass the sender domain's DMARC policy. Combined with the sender below, treat links and attachments as hostile.`,
    })
  } else if (ar.spf === 'fail' && ar.dkim === 'fail') {
    alerts.push({
      severity: 'high',
      title: 'SPF and DKIM both failed',
      detail: 'Neither SPF nor DKIM validated for this message — the sender was not authenticated by its own domain.',
    })
  } else if (ar.spf === 'fail' || ar.dkim === 'fail') {
    alerts.push({
      severity: 'medium',
      title: `${ar.spf === 'fail' ? 'SPF' : 'DKIM'} failed`,
      detail: 'One authentication mechanism failed. Verify the sender before acting on this message.',
    })
  }

  // 2. Display-name claims an email address that differs from the real sender
  const displayName = m.from.name ?? ''
  const claimed = displayName.match(/[\w.+-]+@[\w.-]+\.\w+/)
  if (claimed && claimed[0].toLowerCase() !== m.from.email.toLowerCase()) {
    alerts.push({
      severity: 'high',
      title: 'Sender name does not match the address',
      detail: `The display name claims "${claimed[0]}" but the message actually comes from ${m.from.email}.`,
    })
  }

  // 3. Brand lookalikes in the sender domain (skeleton match, wrong domain)
  const actual = domainOf(m.from.email)
  for (const b of BRAND_DOMAINS) {
    if (b.domains.includes(actual)) break
    for (const d of b.domains) {
      if (actual && d !== actual && skeleton(actual) === skeleton(d)) {
        alerts.push({
          severity: 'high',
          title: 'Lookalike sender domain',
          detail: `"${actual}" closely mimics "${d}" (typical of phishing). ${b.brand} will never send from a lookalike domain.`,
        })
        break
      }
    }
  }

  // 4. High spam score (backend classifier, ingest pipeline)
  if (m.spam_score >= 8) {
    alerts.push({
      severity: 'high',
      title: 'High spam score',
      detail: `The spam classifier scored this message ${m.spam_score.toFixed(1)} / 10.`,
    })
  } else if (m.spam_score >= 5) {
    alerts.push({
      severity: 'medium',
      title: 'Elevated spam score',
      detail: `The spam classifier scored this message ${m.spam_score.toFixed(1)} / 10.`,
    })
  }

  return alerts
}
