/**
 * Centralised query keys. Every query and invalidation goes through here so
 * that "mark read" invalidating a folder badge is a one-line call, not an
 * archaeology exercise.
 */
export const qk = {
  me: () => ['me'] as const,

  mailboxes: () => ['mailboxes'] as const,
  mailbox: (id: string) => ['mailbox', id] as const,
  folders: (mailboxId: string) => ['folders', mailboxId] as const,

  labels: () => ['labels'] as const,

  // Prefix key for all message-list queries of a mailbox. Used for invalidation.
  messagesPrefix: (mailboxId: string) => ['messages', mailboxId] as const,
  messages: (
    mailboxId: string,
    folderId: string,
    search: { q: string; isUnread: boolean; hasAttachment: boolean },
  ) => ['messages', mailboxId, folderId, search] as const,

  message: (id: string) => ['message', id] as const,
  thread: (id: string) => ['thread', id] as const,

  privacySettings: () => ['privacy-settings'] as const,
  identities: (mailboxId: string) => ['identities', mailboxId] as const,
} as const;
