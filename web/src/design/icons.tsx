import { forwardRef } from 'react';

export type IconName =
  | 'inbox' | 'sent' | 'drafts' | 'archive' | 'spam' | 'trash' | 'folder' | 'folder-plus'
  | 'star' | 'star-filled' | 'pin' | 'attachment' | 'reply' | 'reply-all' | 'forward'
  | 'delete' | 'mail-open' | 'mail' | 'search' | 'filter' | 'chevron-down' | 'chevron-right'
  | 'chevron-left' | 'close' | 'more' | 'refresh' | 'external-link' | 'alert-triangle'
  | 'shield-check' | 'lock' | 'eye-off' | 'eye' | 'image' | 'check' | 'check-circle'
  | 'minus' | 'plus' | 'download' | 'print' | 'clock' | 'info' | 'x-circle'
  | 'menu' | 'label' | 'sparkles' | 'tag';

const PATHS: Record<IconName, string> = {
  inbox: 'M3 13h4l2 3h6l2-3h4M5 5h14l3 7v6a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2v-6l3-7z',
  sent: 'M22 2 11 13M22 2l-7 20-4-9-9-4 20-7z',
  drafts: 'M12 3v12M7 8l5-5 5 5M4 15v4a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-4',
  archive: 'M4 5h16v4H4zM4 9v10h16V9M10 13h4',
  spam: 'M12 3 2 21h20L12 3zM12 9v5M12 17h0',
  trash: 'M4 7h16M9 7V5a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2v2M6 7v13h12V7M10 11v6M14 11v6',
  folder: 'M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7z',
  'folder-plus': 'M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7zM12 10v6M9 13h6',
  star: 'm12 3 3 6 6 1-4.5 4 1 6L12 17l-5.5 3 1-6L3 10l6-1 3-6z',
  'star-filled': 'm12 3 3 6 6 1-4.5 4 1 6L12 17l-5.5 3 1-6L3 10l6-1 3-6z',
  pin: 'M12 2v8M8 10h8M12 10v8l-4 4',
  attachment: 'M21 11.5 11 21a5.5 5.5 0 0 1-8-8L13 2.5a3.5 3.5 0 0 1 5 5L8 17a1.5 1.5 0 0 1-2-2L16 5',
  reply: 'M9 11V6l-6 6 6 6v-5h6a6 6 0 0 1 6 6v-2a8 8 0 0 0-8-8H9z',
  'reply-all': 'M7 9V5l-4 4 4 4V9h5c4 0 7 3 7 7M14 9h1c3 0 6 3 6 6v2M4 4l5 4',
  forward: 'M15 11V6l6 6-6 6v-5h-6a6 6 0 0 0-6 6v-2a8 8 0 0 1 8-8h4z',
  delete: 'M4 7h16M9 7V5a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2v2M6 7v13h12V7',
  'mail-open': 'M3 12V7l9-5 9 5v5M3 12l9 6 9-6M3 12v9h18v-9',
  mail: 'M3 6h18v12H3zM3 6l9 7 9-7',
  search: 'M11 19a8 8 0 1 0 0-16 8 8 0 0 0 0 16zM21 21l-4.35-4.35',
  filter: 'M4 5h16l-6 8v6l-4 2v-8L4 5z',
  'chevron-down': 'm6 9 6 6 6-6',
  'chevron-right': 'm9 6 6 6-6 6',
  'chevron-left': 'm15 6-6 6 6 6',
  close: 'M18 6 6 18M6 6l12 12',
  more: 'M12 6h.01M12 12h.01M12 18h.01',
  refresh: 'M21 12a9 9 0 0 1-15.5 6.2M3 12a9 9 0 0 1 15.5-6.2M3 5v4h4M21 19v-4h-4',
  'external-link': 'M14 4h6v6M20 4l-9 9M5 6h6M5 6v14h14v-6',
  'alert-triangle': 'm12 3-10 18h20L12 3zM12 10v5M12 18h0',
  'shield-check': 'M12 3 4 6v6c0 5 3 8 8 9 5-1 8-4 8-9V6l-8-3zM9 12l2 2 4-4',
  lock: 'M5 11h14v10H5zM8 11V8a4 4 0 1 1 8 0v3',
  'eye-off': 'M3 3l18 18M10.6 10.6a2 2 0 0 0 2.8 2.8M9.9 5.1A10 10 0 0 1 12 5c7 0 10 7 10 7a17 17 0 0 1-3.3 4.3M6.3 6.3A17 17 0 0 0 2 12s3 7 10 7a9 9 0 0 0 5.7-1.9',
  eye: 'M2 12s3-7 10-7 10 7 10 7-3 7-10 7S2 12 2 12zM12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z',
  image: 'M3 5h18v14H3zM3 16l5-5 4 4 3-3 6 6M9 10a1 1 0 1 0 0-2 1 1 0 0 0 0 2z',
  check: 'm5 13 4 4L19 7',
  'check-circle': 'M22 11.1V12a10 10 0 1 1-5.9-9.1M22 4 12 14l-3-3',
  minus: 'M5 12h14',
  plus: 'M12 5v14M5 12h14',
  download: 'M12 3v13M6 10l6 6 6-6M4 21h16',
  print: 'M6 9V3h12v6M6 18H4a2 2 0 0 1-2-2v-4a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v4a2 2 0 0 1-2 2h-2M6 14h12v8H6z',
  clock: 'M12 7v5l3 2M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0z',
  info: 'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0zM12 8h0M11 12h1v5h1',
  'x-circle': 'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0zM9 9l6 6M15 9l-6 6',
  menu: 'M3 6h18M3 12h18M3 18h18',
  label: 'M3 7a2 2 0 0 1 2-2h9l7 7-7 7H5a2 2 0 0 1-2-2V7zM8 12h.01',
  tag: 'M3 7a2 2 0 0 1 2-2h9l7 7-7 7H5a2 2 0 0 1-2-2V7z',
  sparkles: 'm12 3 1.5 4.5L18 9l-4.5 1.5L12 15l-1.5-4.5L6 9l4.5-1.5L12 3zM5 17l.8 2.2L8 20l-2.2.8L5 23l-.8-2.2L2 20l2.2-.8L5 17z',
};

export interface IconProps {
  name: IconName;
  size?: number;
  filled?: boolean;
  className?: string;
  'aria-label'?: string;
}

export const Icon = forwardRef<SVGSVGElement, IconProps>(function Icon(
  { name, size = 16, filled, className, 'aria-label': ariaLabel },
  ref,
) {
  const d = PATHS[name];
  const filledIcon = filled ?? name === 'star-filled';
  return (
    <svg
      ref={ref}
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill={filledIcon ? 'currentColor' : 'none'}
      stroke="currentColor"
      strokeWidth={filledIcon ? 0 : 1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      role={ariaLabel ? 'img' : undefined}
      aria-label={ariaLabel}
      aria-hidden={ariaLabel ? undefined : true}
    >
      <path d={d} />
    </svg>
  );
});
