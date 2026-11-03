import { useEffect, type ButtonHTMLAttributes, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes, type TextareaHTMLAttributes } from 'react'
import { X, Loader2 } from 'lucide-react'

export const cx = (...c: (string | false | null | undefined)[]) => c.filter(Boolean).join(' ')

type BtnProps = ButtonHTMLAttributes<HTMLButtonElement> & { variant?: 'primary' | 'secondary' | 'ghost' | 'danger' | 'dark' | 'platform'; size?: 'sm' | 'md'; loading?: boolean }
export function Button({ variant = 'secondary', size = 'md', loading, className, children, disabled, ...p }: BtnProps) {
  const v = {
    primary: 'bg-brand-600 text-white hover:bg-brand-700 border-brand-600',
    secondary: 'bg-white text-ink border-line hover:bg-gray-50',
    ghost: 'bg-transparent border-transparent text-brand-600 hover:bg-brand-50',
    danger: 'bg-white text-red-700 border-red-200 hover:bg-red-50',
    dark: 'bg-transparent text-white border-white/25 hover:bg-white/10',
    platform: 'bg-platform text-white border-platform hover:opacity-90',
  }[variant]
  return (
    <button
      {...p}
      disabled={disabled || loading}
      className={cx('inline-flex items-center justify-center gap-1.5 rounded-lg border font-medium transition-colors disabled:opacity-50 disabled:cursor-not-allowed whitespace-nowrap',
        size === 'sm' ? 'h-8 px-3 text-[13px]' : 'h-10 px-4 text-sm', v, className)}
    >
      {loading && <Loader2 className="size-4 animate-spin" />}
      {children}
    </button>
  )
}

export function Field({ label, error, hint, children, className, required }: { label?: ReactNode; error?: string; hint?: ReactNode; children: ReactNode; className?: string; required?: boolean }) {
  return (
    <label className={cx('block', className)}>
      {label && <span className="mb-1.5 block text-[13px] font-medium text-ink">{label}{required && <span className="text-red-600"> *</span>}</span>}
      {children}
      {error ? <span className="mt-1 block text-xs text-red-600">{error}</span> : hint ? <span className="mt-1 block text-xs text-muted">{hint}</span> : null}
    </label>
  )
}

const hasWidth = (c?: string) => !!c && /(^|\s)!?(max-)?w-/.test(c)
const inputCls = 'rounded-lg border border-line bg-white px-3 text-sm text-ink placeholder:text-gray-400 focus:border-brand-500 focus:outline-none focus:ring-2 focus:ring-brand-100 disabled:bg-gray-50'
export const Input = ({ className, invalid, ...p }: InputHTMLAttributes<HTMLInputElement> & { invalid?: boolean }) => (
  <input {...p} className={cx(inputCls, !hasWidth(className) && 'w-full', 'h-10', invalid && 'border-red-400', className)} />
)
export const Textarea = ({ className, ...p }: TextareaHTMLAttributes<HTMLTextAreaElement>) => <textarea {...p} className={cx(inputCls, 'w-full py-2 min-h-20', className)} />
export const Select = ({ className, children, ...p }: SelectHTMLAttributes<HTMLSelectElement>) => (
  <select {...p} className={cx(inputCls, !hasWidth(className) && 'w-full', 'h-10 pr-8', className)}>{children}</select>
)

export function Checkbox({ checked, onChange, label, disabled }: { checked: boolean; onChange: (v: boolean) => void; label?: ReactNode; disabled?: boolean }) {
  return (
    <label className={cx('inline-flex items-center gap-2 text-sm', disabled ? 'opacity-60' : 'cursor-pointer')}>
      <input type="checkbox" className="size-4 rounded border-gray-300 accent-brand-600" checked={checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)} />
      {label}
    </label>
  )
}

export function Card({ title, action, children, className, bodyClass }: { title?: ReactNode; action?: ReactNode; children: ReactNode; className?: string; bodyClass?: string }) {
  return (
    <section className={cx('rounded-xl border border-line bg-white', className)}>
      {(title || action) && (
        <div className="flex items-center justify-between gap-3 px-5 pt-4 pb-3">
          <h2 className="text-[15px] font-semibold">{title}</h2>
          {action}
        </div>
      )}
      <div className={cx(title || action ? 'px-5 pb-5' : 'p-5', bodyClass)}>{children}</div>
    </section>
  )
}

export type Tone = 'green' | 'red' | 'blue' | 'purple' | 'gray' | 'orange' | 'yellow' | 'teal'
const tones: Record<Tone, string> = {
  green: 'bg-green-100 text-green-800', red: 'bg-red-100 text-red-800', blue: 'bg-indigo-100 text-indigo-800',
  purple: 'bg-purple-100 text-purple-800', gray: 'bg-gray-100 text-gray-700', orange: 'bg-orange-100 text-orange-800',
  yellow: 'bg-amber-100 text-amber-800', teal: 'bg-teal-100 text-teal-800',
}
export const Badge = ({ tone = 'gray', children, className }: { tone?: Tone; children: ReactNode; className?: string }) => (
  <span className={cx('inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-semibold whitespace-nowrap', tones[tone], className)}>{children}</span>
)

// Единые цвета статусов для всех экранов (ТЗ 29.3).
export const studentStatus: Record<string, [string, Tone]> = {
  registration: ['Регистрация', 'gray'], active: ['Занимается', 'green'], frozen: ['Заморожен', 'blue'],
  graduated: ['Выпускник', 'teal'], inactive: ['Не занимается', 'orange'], archived: ['Архив', 'gray'],
}
export const lessonState: Record<string, [string, Tone]> = {
  done: ['Проведено', 'green'], needs_marking: ['Требует отметки', 'red'], in_progress: ['Идёт сейчас', 'blue'],
  trial: ['Пробное', 'purple'], planned: ['Запланировано', 'gray'], cancelled: ['Отменено', 'gray'], needs_decision: ['Требует решения', 'yellow'],
}
export const attendanceStatus: Record<string, [string, string, Tone]> = {
  present: ['Был', 'П', 'green'], late: ['Опоздал', 'О', 'yellow'], absent: ['Не был', 'Н', 'red'], excused: ['Уваж.', 'У', 'blue'], trial: ['Пробный', 'Пр', 'purple'],
}
export const leadStages: Record<string, [string, Tone]> = {
  new: ['Новое обращение', 'blue'], contacted: ['Связались', 'gray'], trial_scheduled: ['Записан на пробное', 'purple'],
  trial_done: ['Пробное проведено', 'teal'], won: ['Оплатил', 'green'], lost: ['Отказ', 'red'],
}
export const paymentMethods: Record<string, string> = { kaspi: 'Kaspi перевод', cash: 'Наличные', card: 'Карта', transfer: 'Банковский перевод', online: 'Онлайн-оплата', other: 'Другое' }

export const StatusBadge = ({ map, value }: { map: Record<string, [string, Tone]>; value: string }) => {
  const [l, t] = map[value] ?? [value, 'gray' as Tone]
  return <Badge tone={t}>{l}</Badge>
}

export function Modal({ open, onClose, title, children, footer, wide }: { open: boolean; onClose: () => void; title: ReactNode; children: ReactNode; footer?: ReactNode; wide?: boolean | 'xl' }) {
  useEffect(() => {
    if (!open) return
    const h = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [open, onClose])
  if (!open) return null
  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-gray-900/40 p-4 pt-[6vh]" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div role="dialog" aria-modal className={cx('w-full rounded-2xl bg-white shadow-xl', wide === 'xl' ? 'max-w-5xl' : wide ? 'max-w-3xl' : 'max-w-lg')}>
        <div className="flex items-center justify-between border-b border-line px-6 py-4">
          <h3 className="text-base font-semibold">{title}</h3>
          <button onClick={onClose} className="rounded-md p-1 text-muted hover:bg-gray-100" aria-label="Закрыть"><X className="size-5" /></button>
        </div>
        <div className="px-6 py-5">{children}</div>
        {footer && <div className="flex justify-end gap-2 border-t border-line px-6 py-4">{footer}</div>}
      </div>
    </div>
  )
}

export function Drawer({ open, onClose, title, children }: { open: boolean; onClose: () => void; title: ReactNode; children: ReactNode }) {
  if (!open) return null
  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-gray-900/30" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <aside className="flex h-full w-full max-w-xl flex-col bg-white shadow-xl">
        <div className="flex items-center justify-between border-b border-line px-6 py-4">
          <h3 className="text-base font-semibold">{title}</h3>
          <button onClick={onClose} className="rounded-md p-1 text-muted hover:bg-gray-100" aria-label="Закрыть"><X className="size-5" /></button>
        </div>
        <div className="flex-1 overflow-y-auto px-6 py-5">{children}</div>
      </aside>
    </div>
  )
}

export function Tabs<T extends string>({ value, onChange, items, className }: { value: T; onChange: (v: T) => void; items: { value: T; label: ReactNode; count?: number }[]; className?: string }) {
  return (
    <div className={cx('flex gap-1 border-b border-line', className)}>
      {items.map((i) => (
        <button key={i.value} onClick={() => onChange(i.value)}
          className={cx('-mb-px border-b-2 px-3 pb-2.5 pt-1 text-sm font-medium transition-colors', value === i.value ? 'border-brand-600 text-ink' : 'border-transparent text-muted hover:text-ink')}>
          {i.label}{i.count != null && <span className="ml-1.5 text-xs text-muted">{i.count}</span>}
        </button>
      ))}
    </div>
  )
}

export function Segmented<T extends string>({ value, onChange, items }: { value: T; onChange: (v: T) => void; items: { value: T; label: ReactNode }[] }) {
  return (
    <div className="inline-flex rounded-lg bg-gray-100 p-1">
      {items.map((i) => (
        <button key={i.value} onClick={() => onChange(i.value)}
          className={cx('rounded-md px-3 py-1.5 text-sm font-medium', value === i.value ? 'bg-white text-ink shadow-sm' : 'text-muted hover:text-ink')}>
          {i.label}
        </button>
      ))}
    </div>
  )
}

export const Spinner = ({ className }: { className?: string }) => (
  <div className={cx('flex items-center justify-center py-12 text-muted', className)}><Loader2 className="size-6 animate-spin" /></div>
)

export function Empty({ title, text, action }: { title: string; text?: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center px-6 py-14 text-center">
      <div className="text-[15px] font-semibold">{title}</div>
      {text && <div className="mt-1 max-w-md text-sm text-muted">{text}</div>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  )
}

export function Avatar({ name, size = 'md', tone = 'brand' }: { name: string; size?: 'sm' | 'md' | 'lg'; tone?: 'brand' | 'gray' | 'teal' }) {
  const s = { sm: 'size-8 text-xs', md: 'size-10 text-sm', lg: 'size-14 text-lg' }[size]
  const t = { brand: 'bg-brand-100 text-brand-700', gray: 'bg-gray-100 text-gray-700', teal: 'bg-teal-100 text-teal-800' }[tone]
  const initials = name.split(/\s+/).filter(Boolean).slice(0, 2).map((p) => p[0]).join('').toUpperCase()
  return <div className={cx('flex shrink-0 items-center justify-center rounded-full font-semibold', s, t)}>{initials}</div>
}

export function Table({ head, children, className }: { head: ReactNode[]; children: ReactNode; className?: string }) {
  return (
    <div className={cx('overflow-x-auto', className)}>
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-line text-left text-[11px] font-semibold uppercase tracking-wide text-muted">
            {head.map((h, i) => <th key={i} className="whitespace-nowrap px-4 py-3 font-semibold">{h}</th>)}
          </tr>
        </thead>
        <tbody className="divide-y divide-line">{children}</tbody>
      </table>
    </div>
  )
}

export function Stat({ label, value, sub, tone, onClick }: { label: string; value: ReactNode; sub?: ReactNode; tone?: 'warn'; onClick?: () => void }) {
  return (
    <div onClick={onClick} className={cx('rounded-xl border p-5', tone === 'warn' ? 'border-orange-200 bg-orange-50' : 'border-line bg-white', onClick && 'cursor-pointer hover:border-gray-300')}>
      <div className={cx('text-[13px]', tone === 'warn' ? 'text-orange-800' : 'text-muted')}>{label}</div>
      <div className={cx('mt-2 text-[28px] font-bold leading-none tabular', tone === 'warn' && 'text-orange-900')}>{value}</div>
      {sub && <div className="mt-3 text-[13px]">{sub}</div>}
    </div>
  )
}

export function Banner({ tone, icon, children, action }: { tone: 'red' | 'blue' | 'orange' | 'teal' | 'yellow'; icon?: ReactNode; children: ReactNode; action?: ReactNode }) {
  const t = { red: 'border-red-200 bg-red-50 text-red-900', blue: 'border-blue-200 bg-blue-50 text-blue-900', orange: 'border-orange-200 bg-orange-50 text-orange-900',
    teal: 'border-teal-200 bg-teal-50 text-teal-900', yellow: 'border-amber-200 bg-amber-50 text-amber-900' }[tone]
  return (
    <div className={cx('flex items-start gap-3 rounded-xl border px-4 py-3 text-sm', t)}>
      {icon && <span className="mt-0.5 shrink-0">{icon}</span>}
      <div className="flex-1">{children}</div>
      {action}
    </div>
  )
}

export function Toast({ text, onDone }: { text: string; onDone: () => void }) {
  useEffect(() => { const t = setTimeout(onDone, 3000); return () => clearTimeout(t) }, [onDone])
  return <div className="fixed bottom-6 left-1/2 z-[60] -translate-x-1/2 rounded-lg bg-gray-900 px-4 py-2.5 text-sm text-white shadow-lg">{text}</div>
}
