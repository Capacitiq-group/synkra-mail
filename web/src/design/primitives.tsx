import {
  forwardRef, useEffect, useId, useRef, useState,
  type ButtonHTMLAttributes, type InputHTMLAttributes, type ReactNode,
  type SelectHTMLAttributes, type TextareaHTMLAttributes,
} from 'react';
import clsx from 'clsx';
import { useEscapeKey, useOnClickOutside } from '@/hooks';

// ===== Button =====
type Variant = 'primary' | 'secondary' | 'ghost' | 'danger' | 'link';
type Size = 'sm' | 'md' | 'lg' | 'icon';

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  size?: Size;
  loading?: boolean;
  iconLeft?: ReactNode;
  iconRight?: ReactNode;
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = 'secondary', size = 'md', loading, iconLeft, iconRight, className, children, disabled, ...rest },
  ref,
) {
  const base = 'inline-flex items-center justify-center gap-2 rounded font-medium transition-colors disabled:opacity-60 disabled:cursor-not-allowed';
  const variants: Record<Variant, string> = {
    primary: 'bg-accent text-white hover:bg-accent-hover',
    secondary: 'bg-surface text-ink border border-line hover:bg-canvas',
    ghost: 'text-ink hover:bg-canvas',
    danger: 'bg-danger text-white hover:opacity-90',
    link: 'text-accent hover:underline px-0 py-0',
  };
  const sizes: Record<Size, string> = {
    sm: 'px-2.5 py-1 text-xs',
    md: 'px-3 py-1.5 text-sm',
    lg: 'px-4 py-2 text-base',
    icon: 'p-1.5',
  };
  return (
    <button
      ref={ref}
      disabled={disabled || loading}
      className={clsx(base, variants[variant], sizes[size], className)}
      {...rest}
    >
      {loading ? <Spinner size={14} /> : iconLeft}
      {children}
      {iconRight}
    </button>
  );
});

// ===== Spinner =====
export function Spinner({ size = 16, className }: { size?: number; className?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" className={clsx('animate-spin', className)} aria-hidden>
      <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="3" fill="none" strokeDasharray="42" strokeDashoffset="16" strokeLinecap="round" />
    </svg>
  );
}

// ===== Field =====
interface FieldProps { label?: string; hint?: string; error?: string; required?: boolean; children: ReactNode; htmlFor?: string }
export function Field({ label, hint, error, required, children, htmlFor }: FieldProps) {
  return (
    <div className="space-y-1">
      {label && <label htmlFor={htmlFor} className="block text-xs font-medium text-ink-soft">{label}{required && <span className="text-danger"> *</span>}</label>}
      {children}
      {error ? <p className="text-xs text-danger">{error}</p> : hint ? <p className="text-xs text-ink-mute">{hint}</p> : null}
    </div>
  );
}

// ===== Input =====
export interface InputProps extends InputHTMLAttributes<HTMLInputElement> { invalid?: boolean }
export const Input = forwardRef<HTMLInputElement, InputProps>(function Input({ className, invalid, ...rest }, ref) {
  return (
    <input
      ref={ref}
      aria-invalid={invalid || undefined}
      className={clsx(
        'w-full rounded border px-3 py-2 text-sm bg-surface text-ink placeholder:text-ink-mute',
        invalid ? 'border-danger' : 'border-line',
        'focus:outline-none focus:ring-2 focus:ring-accent',
        className,
      )}
      {...rest}
    />
  );
});

// ===== Textarea =====
export interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> { invalid?: boolean }
export const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(function Textarea({ className, invalid, ...rest }, ref) {
  return (
    <textarea
      ref={ref}
      aria-invalid={invalid || undefined}
      className={clsx(
        'w-full rounded border px-3 py-2 text-sm bg-surface text-ink placeholder:text-ink-mute',
        invalid ? 'border-danger' : 'border-line',
        'focus:outline-none focus:ring-2 focus:ring-accent',
        className,
      )}
      {...rest}
    />
  );
});

// ===== Select =====
export interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> { invalid?: boolean }
export const Select = forwardRef<HTMLSelectElement, SelectProps>(function Select({ className, invalid, children, ...rest }, ref) {
  return (
    <select
      ref={ref}
      aria-invalid={invalid || undefined}
      className={clsx(
        'w-full rounded border px-3 py-2 text-sm bg-surface text-ink',
        invalid ? 'border-danger' : 'border-line',
        'focus:outline-none focus:ring-2 focus:ring-accent',
        className,
      )}
      {...rest}
    >
      {children}
    </select>
  );
});

// ===== Checkbox =====
interface CheckboxProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'type'> { label?: string }
export const Checkbox = forwardRef<HTMLInputElement, CheckboxProps>(function Checkbox({ label, className, ...rest }, ref) {
  const id = useId();
  return (
    <label className={clsx('inline-flex items-center gap-2 text-sm cursor-pointer', className)} htmlFor={rest.id || id}>
      <input ref={ref} id={rest.id || id} type="checkbox" className="rounded border-line text-accent focus:ring-accent" {...rest} />
      {label}
    </label>
  );
});

// ===== Switch =====
interface SwitchProps { checked: boolean; onChange: (v: boolean) => void; label?: string; disabled?: boolean }
export function Switch({ checked, onChange, label, disabled }: SwitchProps) {
  const id = useId();
  return (
    <label htmlFor={id} className="inline-flex items-center gap-2 cursor-pointer text-sm">
      <button
        id={id}
        role="switch"
        type="button"
        aria-checked={checked}
        disabled={disabled}
        onClick={() => onChange(!checked)}
        className={clsx('relative w-9 h-5 rounded-full transition-colors', checked ? 'bg-accent' : 'bg-line', disabled && 'opacity-50')}
      >
        <span className={clsx('absolute top-0.5 w-4 h-4 bg-white rounded-full transition-transform', checked ? 'translate-x-4' : 'translate-x-0.5')} />
      </button>
      {label}
    </label>
  );
}

// ===== Badge =====
interface BadgeProps { tone?: 'neutral' | 'success' | 'warning' | 'danger' | 'accent'; children: ReactNode; className?: string }
export function Badge({ tone = 'neutral', children, className }: BadgeProps) {
  const tones = {
    neutral: 'bg-canvas text-ink-soft border border-line',
    success: 'bg-success-soft text-success',
    warning: 'bg-warning-soft text-warning',
    danger: 'bg-danger-soft text-danger',
    accent: 'bg-accent-soft text-accent',
  } as const;
  return <span className={clsx('inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium', tones[tone], className)}>{children}</span>;
}

// ===== Avatar =====
import { initials } from '@/lib/format';
interface AvatarProps { name?: string; email?: string; src?: string; size?: number; className?: string }
export function Avatar({ name, email, src, size = 32, className }: AvatarProps) {
  const style = { width: size, height: size, fontSize: Math.max(10, size * 0.4) };
  if (src) return <img src={src} alt="" className={clsx('rounded-full object-cover', className)} style={style} />;
  return (
    <span
      style={style}
      className={clsx('inline-flex items-center justify-center rounded-full bg-accent text-white font-semibold', className)}
      aria-hidden
    >
      {initials(name, email)}
    </span>
  );
}

// ===== Skeleton =====
export function Skeleton({ className }: { className?: string }) {
  return <div className={clsx('animate-pulse bg-line rounded', className)} />;
}

// ===== EmptyState =====
interface EmptyStateProps { title: string; description?: string; action?: ReactNode; icon?: ReactNode }
export function EmptyState({ title, description, action, icon }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center text-center p-8">
      {icon && <div className="mb-3 text-ink-mute">{icon}</div>}
      <h3 className="font-medium">{title}</h3>
      {description && <p className="text-sm text-ink-soft mt-1 max-w-sm">{description}</p>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}

// ===== Modal =====
interface ModalProps {
  open: boolean;
  onClose: () => void;
  title?: string;
  description?: string;
  children: ReactNode;
  footer?: ReactNode;
  size?: 'sm' | 'md' | 'lg';
}
export function Modal({ open, onClose, title, description, children, footer, size = 'md' }: ModalProps) {
  useEscapeKey(onClose, open);
  useEffect(() => {
    if (!open) return;
    const prev = document.activeElement as HTMLElement | null;
    const root = document.getElementById('modal-root');
    if (root) {
      const focusable = root.querySelector<HTMLElement>('[data-autofocus]') || root.querySelector<HTMLElement>('button,input,select,textarea,a[href]');
      focusable?.focus();
    }
    return () => prev?.focus();
  }, [open]);

  if (!open) return null;
  const sizes = { sm: 'max-w-sm', md: 'max-w-md', lg: 'max-w-2xl' } as const;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4" role="dialog" aria-modal aria-labelledby="modal-title">
      <div className="absolute inset-0 bg-black/40 animate-fade-in" onClick={onClose} aria-hidden />
      <div id="modal-root" className={clsx('relative w-full bg-surface rounded-lg shadow-lg animate-slide-up', sizes[size])}>
        <div className="flex items-start gap-3 p-4 border-b border-line">
          <div className="flex-1">
            {title && <h2 id="modal-title" className="font-semibold">{title}</h2>}
            {description && <p className="text-sm text-ink-soft mt-1">{description}</p>}
          </div>
          <button onClick={onClose} className="p-1 rounded hover:bg-canvas" aria-label="Close">
            <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden><path d="M3 3l10 10M13 3L3 13" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" /></svg>
          </button>
        </div>
        <div className="p-4 max-h-[70vh] overflow-y-auto scroll-thin">{children}</div>
        {footer && <div className="p-4 border-t border-line flex justify-end gap-2">{footer}</div>}
      </div>
    </div>
  );
}

// ===== Drawer =====
interface DrawerProps { open: boolean; onClose: () => void; side?: 'left' | 'right'; children: ReactNode; title?: string }
export function Drawer({ open, onClose, side = 'left', title, children }: DrawerProps) {
  useEscapeKey(onClose, open);
  useEffect(() => {
    if (open) {
      const prev = document.body.style.overflow;
      document.body.style.overflow = 'hidden';
      return () => { document.body.style.overflow = prev; };
    }
  }, [open]);
  return (
    <>
      <div className={clsx('fixed inset-0 z-40 bg-black/40 transition-opacity', open ? 'opacity-100' : 'opacity-0 pointer-events-none')} onClick={onClose} aria-hidden />
      <aside
        role="dialog"
        aria-modal
        aria-label={title}
        className={clsx(
          'fixed top-0 bottom-0 z-50 w-72 max-w-[85vw] bg-surface shadow-lg flex flex-col transition-transform',
          side === 'left' ? 'left-0' : 'right-0',
          open ? 'translate-x-0' : side === 'left' ? '-translate-x-full' : 'translate-x-full',
        )}
      >
        <header className="flex items-center justify-between p-3 border-b border-line">
          <h2 className="font-semibold">{title}</h2>
          <button onClick={onClose} className="p-1 rounded hover:bg-canvas" aria-label="Close">
            <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden><path d="M3 3l10 10M13 3L3 13" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" /></svg>
          </button>
        </header>
        <div className="flex-1 overflow-y-auto scroll-thin">{children}</div>
      </aside>
    </>
  );
}

// ===== ConfirmDialog =====
interface ConfirmDialogProps {
  open: boolean; onCancel: () => void; onConfirm: () => void;
  title: string; description?: string; confirmLabel?: string; danger?: boolean; busy?: boolean;
}
export function ConfirmDialog({ open, onCancel, onConfirm, title, description, confirmLabel = 'Confirm', danger, busy }: ConfirmDialogProps) {
  return (
    <Modal
      open={open}
      onClose={onCancel}
      title={title}
      description={description}
      size="sm"
      footer={
        <>
          <Button variant="secondary" onClick={onCancel} disabled={busy}>Cancel</Button>
          <Button variant={danger ? 'danger' : 'primary'} onClick={onConfirm} loading={busy} data-autofocus>
            {confirmLabel}
          </Button>
        </>
      }
    >
      <p className="text-sm text-ink-soft">This action cannot be undone.</p>
    </Modal>
  );
}

// ===== Menu =====
export function Menu({ trigger, children }: { trigger: ReactNode; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useOnClickOutside(ref, () => setOpen(false));
  useEscapeKey(() => setOpen(false), open);
  return (
    <div className="relative" ref={ref}>
      <div onClick={() => setOpen((v) => !v)}>{trigger}</div>
      {open && (
        <div role="menu" className="absolute right-0 mt-1 z-40 min-w-48 rounded-md border border-line bg-surface shadow-md animate-slide-down p-1">
          <div onClick={() => setOpen(false)}>{children}</div>
        </div>
      )}
    </div>
  );
}
export function MenuItem({ children, onClick, danger }: { children: ReactNode; onClick?: () => void; danger?: boolean }) {
  return (
    <button
      role="menuitem"
      onClick={onClick}
      className={clsx('w-full text-left px-3 py-1.5 text-sm rounded hover:bg-canvas', danger && 'text-danger')}
    >
      {children}
    </button>
  );
}

// ===== Tabs =====
interface TabItem { id: string; label: string; badge?: number }
interface TabsProps { items: TabItem[]; value: string; onChange: (id: string) => void }
export function Tabs({ items, value, onChange }: TabsProps) {
  return (
    <div role="tablist" className="flex border-b border-line">
      {items.map((it) => (
        <button
          key={it.id}
          role="tab"
          aria-selected={value === it.id}
          onClick={() => onChange(it.id)}
          className={clsx(
            'px-4 py-2 text-sm border-b-2 -mb-px',
            value === it.id ? 'border-accent text-accent font-medium' : 'border-transparent text-ink-soft hover:text-ink',
          )}
        >
          {it.label}
          {typeof it.badge === 'number' && it.badge > 0 && (
            <span className="ml-1 text-xs rounded-full bg-accent text-white px-1.5 py-0.5">{it.badge}</span>
          )}
        </button>
      ))}
    </div>
  );
}

// ===== Combobox (recipient / contact autocomplete) =====
interface ComboboxOption { value: string; label: string; sublabel?: string }
interface ComboboxProps {
  options: ComboboxOption[];
  value: string;
  onChange: (v: string) => void;
  onSelect?: (o: ComboboxOption) => void;
  placeholder?: string;
  loading?: boolean;
}
export function Combobox({ options, value, onChange, onSelect, placeholder, loading }: ComboboxProps) {
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const ref = useRef<HTMLDivElement>(null);
  useOnClickOutside(ref, () => setOpen(false));

  useEffect(() => { setActive(0); }, [value]);
  const filtered = value ? options : options.slice(0, 8);

  return (
    <div className="relative" ref={ref}>
      <Input
        value={value}
        onChange={(e) => { onChange(e.target.value); setOpen(true); }}
        onFocus={() => setOpen(true)}
        onKeyDown={(e) => {
          if (e.key === 'ArrowDown') { e.preventDefault(); setActive((a) => Math.min(filtered.length - 1, a + 1)); }
          else if (e.key === 'ArrowUp') { e.preventDefault(); setActive((a) => Math.max(0, a - 1)); }
          else if (e.key === 'Enter' && open && filtered[active]) {
            e.preventDefault();
            onSelect?.(filtered[active]);
            setOpen(false);
          }
        }}
        placeholder={placeholder}
      />
      {open && (filtered.length > 0 || loading) && (
        <div className="absolute z-30 mt-1 w-full max-h-64 overflow-y-auto scroll-thin bg-surface border border-line rounded shadow-md p-1">
          {loading && <div className="p-2 text-xs text-ink-soft">Loading…</div>}
          {filtered.map((o, i) => (
            <button
              key={o.value}
              type="button"
              onClick={() => { onSelect?.(o); setOpen(false); }}
              className={clsx('w-full text-left px-2 py-1 rounded', i === active ? 'bg-canvas' : 'hover:bg-canvas')}
            >
              <div className="text-sm">{o.label}</div>
              {o.sublabel && <div className="text-xs text-ink-soft">{o.sublabel}</div>}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// ===== Date + time picker (datetime-local backed, timezone-aware label) =====
interface DateTimePickerProps { value: string; onChange: (iso: string) => void; label?: string; min?: string }
export function DateTimePicker({ value, onChange, label, min }: DateTimePickerProps) {
  const local = value ? new Date(value) : new Date();
  const pad = (n: number) => String(n).padStart(2, '0');
  const localValue = `${local.getFullYear()}-${pad(local.getMonth() + 1)}-${pad(local.getDate())}T${pad(local.getHours())}:${pad(local.getMinutes())}`;
  return (
    <Field label={label}>
      <input
        type="datetime-local"
        value={localValue}
        min={min}
        onChange={(e) => {
          const d = e.target.value ? new Date(e.target.value) : null;
          onChange(d ? d.toISOString() : '');
        }}
        className="w-full rounded border border-line px-3 py-2 text-sm bg-surface"
      />
      <p className="text-xs text-ink-mute mt-1">{Intl.DateTimeFormat().resolvedOptions().timeZone}</p>
    </Field>
  );
}

// ===== Breadcrumbs =====
interface Crumb { label: string; to?: string }
export function Breadcrumbs({ items }: { items: Crumb[] }) {
  return (
    <nav aria-label="Breadcrumbs" className="text-sm text-ink-soft">
      {items.map((c, i) => (
        <span key={i}>
          {i > 0 && <span className="mx-1 text-ink-mute">/</span>}
          {c.to ? <a href={c.to} className="hover:text-ink">{c.label}</a> : <span className="text-ink">{c.label}</span>}
        </span>
      ))}
    </nav>
  );
}

// ===== Progress =====
export function ProgressBar({ value, max = 100, className }: { value: number; max?: number; className?: string }) {
  const pct = Math.min(100, (value / max) * 100);
  return (
    <div className={clsx('h-2 w-full bg-line rounded overflow-hidden', className)} role="progressbar" aria-valuenow={value} aria-valuemax={max}>
      <div className="h-full bg-accent" style={{ width: `${pct}%` }} />
    </div>
  );
}
