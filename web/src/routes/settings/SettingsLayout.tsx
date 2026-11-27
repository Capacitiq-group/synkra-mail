import { Link } from 'react-router-dom';
import { Icon } from '@/design/icons';
import { usePageTitle } from '@/hooks';

export function SettingsHeader({
  title,
  description,
  back,
}: {
  title: string;
  description?: string;
  back?: { to: string; label: string };
}) {
  usePageTitle(title);
  return (
    <header className="px-4 md:px-8 py-4 border-b border-line bg-surface">
      {back && (
        <Link to={back.to} className="inline-flex items-center gap-1 text-sm text-ink-soft hover:text-ink mb-2">
          <Icon name="chevron-left" size={14} /> {back.label}
        </Link>
      )}
      <h1 className="text-lg font-semibold">{title}</h1>
      {description && <p className="text-sm text-ink-soft mt-1">{description}</p>}
    </header>
  );
}

export function SettingsPage({ children, maxWidth = 'lg' }: { children: React.ReactNode; maxWidth?: 'md' | 'lg' | 'xl' }) {
  const widths = { md: 'max-w-2xl', lg: 'max-w-3xl', xl: 'max-w-5xl' } as const;
  return (
    <div className="h-full overflow-y-auto scroll-thin bg-canvas">
      <div className={`mx-auto ${widths[maxWidth]} bg-surface min-h-full`}>{children}</div>
    </div>
  );
}

export function SettingsContent({ children }: { children: React.ReactNode }) {
  return <div className="p-4 md:p-8">{children}</div>;
}
