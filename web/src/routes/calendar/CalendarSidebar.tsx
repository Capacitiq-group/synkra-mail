import { useNavigate } from 'react-router-dom';
import { Button } from '@/design';
import { Icon } from '@/design/icons';
import type { Calendar } from '@/lib/types';

interface Props {
  calendars: Calendar[];
  visibleIds: Set<string>;
  onToggle: (id: string) => void;
  onCreate: () => void;
  onManageCalendars: () => void;
  onManageScheduling: () => void;
}

export function CalendarSidebar({ calendars, visibleIds, onToggle, onCreate, onManageCalendars, onManageScheduling }: Props) {
  const navigate = useNavigate();
  return (
    <div className="flex flex-col h-full">
      <div className="p-3 border-b border-line">
        <Button variant="primary" className="w-full justify-center" onClick={onCreate}>
          <Icon name="plus" size={14} /> New event
        </Button>
      </div>

      <nav className="flex-1 overflow-y-auto scroll-thin p-2 space-y-0.5" aria-label="Calendars">
        <h3 className="px-2 pt-1 pb-1 text-xs uppercase text-ink-mute">My calendars</h3>
        {calendars.filter((c) => c.kind === 'personal').map((c) => (
          <CalendarRow key={c.id} calendar={c} visible={visibleIds.has(c.id)} onToggle={() => onToggle(c.id)} />
        ))}
        {calendars.some((c) => c.kind === 'shared') && (
          <h3 className="px-2 pt-3 pb-1 text-xs uppercase text-ink-mute">Shared</h3>
        )}
        {calendars.filter((c) => c.kind === 'shared').map((c) => (
          <CalendarRow key={c.id} calendar={c} visible={visibleIds.has(c.id)} onToggle={() => onToggle(c.id)} />
        ))}
      </nav>

      <div className="border-t border-line p-2 space-y-1">
        <button
          onClick={onManageCalendars}
          className="w-full text-left px-2 py-1 text-xs text-ink-soft hover:text-ink hover:bg-canvas rounded"
        >
          Manage calendars
        </button>
        <button
          onClick={onManageScheduling}
          className="w-full text-left px-2 py-1 text-xs text-ink-soft hover:text-ink hover:bg-canvas rounded"
        >
          Scheduling links
        </button>
        <button
          onClick={() => navigate('/settings/security')}
          className="w-full text-left px-2 py-1 text-xs text-ink-soft hover:text-ink hover:bg-canvas rounded"
        >
          Account & security
        </button>
      </div>
    </div>
  );
}

function CalendarRow({ calendar, visible, onToggle }: { calendar: Calendar; visible: boolean; onToggle: () => void }) {
  return (
    <button
      type="button"
      onClick={onToggle}
      className="w-full flex items-center gap-2 px-2 py-1.5 text-sm rounded hover:bg-canvas text-left"
      aria-pressed={visible}
    >
      <span
        className="w-3 h-3 rounded-sm border"
        style={{ backgroundColor: visible ? calendar.color : 'transparent', borderColor: calendar.color }}
        aria-hidden
      />
      <span className={`flex-1 truncate ${visible ? '' : 'text-ink-mute'}`}>{calendar.name}</span>
    </button>
  );
}
