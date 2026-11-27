import type { Address, Draft, Identity, MessageDetail } from '@/lib/types';

export type ComposeMode = 'new' | 'reply' | 'reply_all' | 'forward';
export type BodyMode = 'rich' | 'plain' | 'html';
export type RecipientField = 'to' | 'cc' | 'bcc';

export interface StagedAttachment {
  id: string;
  filename: string;
  size: number;
  content_type: string;
  uploading?: boolean;
  error?: string;
}

export interface ComposeSnapshot {
  mailboxId: string;
  identityId: string;
  to: Address[];
  cc: Address[];
  bcc: Address[];
  subject: string;
  bodyMode: BodyMode;
  bodyHtml: string;
  bodyText: string;
  attachmentIds: string[];
  attachments: StagedAttachment[];
  inReplyTo?: string;
  references?: string[];
  sourceMessageId?: string;
  composeMode: ComposeMode;
  draftId?: string;
}

export interface DraftInit {
  draft: Draft;
  identities: Identity[];
  mailboxId: string;
}

export interface ReplyInit {
  source: MessageDetail;
  mode: ComposeMode;
  mailboxId: string;
  identityId: string;
  quoted: { html: string; text: string; subject: string; to: Address[]; cc: Address[] };
}
