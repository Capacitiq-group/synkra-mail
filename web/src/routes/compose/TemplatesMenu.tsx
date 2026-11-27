import { useQuery } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { Button, Menu, MenuItem } from '@/design';
import { Icon } from '@/design/icons';
import { useComposeStore } from './useComposeStore';

export function TemplatesMenu() {
  const templates = useQuery({ queryKey: ['templates'], queryFn: api.templates, staleTime: 60_000 });
  const setSubject = useComposeStore((s) => s.setSubject);
  const setBodyHtml = useComposeStore((s) => s.setBodyHtml);
  const setBodyMode = useComposeStore((s) => s.setBodyMode);

  if (!templates.data || templates.data.length === 0) return null;

  return (
    <Menu
      trigger={
        <Button size="sm" variant="ghost" title="Insert template">
          <Icon name="sparkles" size={14} /> Templates
        </Button>
      }
    >
      {templates.data.map((t) => (
        <MenuItem
          key={t.id}
          onClick={() => {
            if (!useComposeStore.getState().subject && t.subject) setSubject(t.subject);
            if (t.body_html) { setBodyMode('rich'); setBodyHtml(t.body_html, t.body_text); }
            else { setBodyMode('plain'); setBodyHtml('', t.body_text); }
          }}
        >
          <div className="text-sm">{t.name}</div>
          {t.subject && <div className="text-xs text-ink-soft truncate">{t.subject}</div>}
        </MenuItem>
      ))}
    </Menu>
  );
}
