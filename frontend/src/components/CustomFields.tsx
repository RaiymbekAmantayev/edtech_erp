// Рендер и ввод пользовательских полей по типу (ТЗ 9.3). Значения хранятся по ID вариантов, деньги — в тиын.
import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { api } from '../lib/api'
import { money, parseMoney, date, phone } from '../lib/format'
import { label, tr, intlLocale } from '../lib/i18n'
import type { CField, CFBlock } from '../lib/types'
import { Badge, Checkbox, Field, Input, Select, Textarea, cx } from './ui'

export function useFields(entity: string, archived = false) {
  return useQuery({
    queryKey: ['custom-fields', entity, archived],
    queryFn: () => api.getMeta<CField[]>(`/custom-fields?entity=${entity}${archived ? '&archived=1' : ''}`),
  })
}

export function useBlocks(entity: string) {
  return useQuery({ queryKey: ['cf-blocks', entity], queryFn: () => api.get<CFBlock[]>(`/custom-field-blocks?entity=${entity}`) })
}

export const fieldTypes: { value: string; label: string }[] = [
  { value: 'string', label: tr('Строка') }, { value: 'text', label: tr('Текст') }, { value: 'integer', label: tr('Число') }, { value: 'money', label: tr('Деньги') },
  { value: 'date', label: tr('Дата') }, { value: 'boolean', label: tr('Да / Нет') }, { value: 'select', label: tr('Список') }, { value: 'multiselect', label: tr('Мультисписок') },
  { value: 'phone', label: tr('Телефон') }, { value: 'url', label: tr('Ссылка') }, { value: 'decimal', label: tr('Дробное') }, { value: 'email', label: 'Email' },
  { value: 'datetime', label: tr('Дата и время') }, { value: 'file', label: tr('Файл') }, { value: 'employee', label: tr('Сотрудник') }, { value: 'relation', label: tr('Связь') },
]
export const typeLabel = (t: string) => fieldTypes.find((x) => x.value === t)?.label ?? t

const optLabel = (f: CField, id: unknown) => label(f.options.find((o) => o.id === id)?.label) || String(id)

export function cfDisplay(f: CField, v: unknown): ReactNode {
  if (v === undefined || v === null || v === '') return <span className="text-muted">—</span>
  if (v === '••••••') return <span className="tracking-widest text-muted">••••••</span>
  switch (f.field_type) {
    case 'select': return optLabel(f, v)
    case 'multiselect':
      return <span className="flex flex-wrap gap-1">{(v as string[]).map((id) => <Badge key={id} tone="gray" className="!font-medium">{optLabel(f, id)}</Badge>)}</span>
    case 'boolean': return v ? tr('Да') : tr('Нет')
    case 'money': return money(Number(v))
    case 'date': return date(String(v))
    case 'datetime': return new Date(String(v)).toLocaleString(intlLocale())
    case 'phone': return phone(String(v))
    case 'url': return <a href={String(v)} target="_blank" rel="noreferrer" className="text-brand-600 hover:underline">{String(v).replace(/^https?:\/\//, '').slice(0, 40)}</a>
  }
  return String(v)
}

export function cfText(f: CField, v: unknown): string {
  if (v === undefined || v === null || v === '') return ''
  if (f.field_type === 'select') return optLabel(f, v)
  if (f.field_type === 'multiselect') return (v as string[]).map((id) => optLabel(f, id)).join(', ')
  if (f.field_type === 'boolean') return v ? tr('Да') : tr('Нет')
  if (f.field_type === 'money') return money(Number(v))
  return String(v)
}

export function CFInput({ field: f, value, onChange, error, required }: { field: CField; value: unknown; onChange: (v: unknown) => void; error?: string; required?: boolean }) {
  const s = f.settings
  const lbl = label(f.label)
  const opts = f.options.filter((o) => !o.archived_at || o.id === value || (Array.isArray(value) && value.includes(o.id)))
  let input: ReactNode
  switch (f.field_type) {
    case 'text':
      input = <Textarea value={(value as string) ?? ''} placeholder={s.placeholder} onChange={(e) => onChange(e.target.value)} />
      break
    case 'integer':
    case 'decimal':
      input = <Input type="number" step={f.field_type === 'integer' ? 1 : 'any'} min={s.min} max={s.max} value={(value as number) ?? ''} placeholder={s.placeholder}
        invalid={!!error} onChange={(e) => onChange(e.target.value === '' ? null : Number(e.target.value))} />
      break
    case 'money':
      input = <Input inputMode="decimal" defaultValue={value != null ? String(Number(value) / 100) : ''} placeholder="0 ₸"
        onChange={(e) => onChange(e.target.value === '' ? null : parseMoney(e.target.value))} />
      break
    case 'date':
      input = <Input type="date" value={(value as string) ?? ''} onChange={(e) => onChange(e.target.value || null)} />
      break
    case 'datetime':
      input = <Input type="datetime-local" value={value ? String(value).slice(0, 16) : ''} onChange={(e) => onChange(e.target.value ? new Date(e.target.value).toISOString() : null)} />
      break
    case 'boolean':
      return (
        <div className="pt-6"><Checkbox checked={!!value} onChange={onChange} label={lbl} />{error && <div className="mt-1 text-xs text-red-600">{error}</div>}</div>
      )
    case 'select':
      input = (
        <Select value={(value as string) ?? ''} onChange={(e) => onChange(e.target.value || null)} className={cx(error && 'border-red-400')}>
          <option value="">{tr('— не выбрано —')}</option>
          {opts.map((o) => <option key={o.id} value={o.id}>{label(o.label)}{o.archived_at ? tr(' (архивный)') : ''}</option>)}
        </Select>
      )
      break
    case 'multiselect': {
      const arr = (value as string[]) ?? []
      input = (
        <div className="flex flex-wrap gap-1.5">
          {opts.map((o) => {
            const on = arr.includes(o.id)
            return (
              <button type="button" key={o.id} onClick={() => onChange(on ? arr.filter((x) => x !== o.id) : [...arr, o.id])}
                className={cx('rounded-full border px-2.5 py-1 text-xs font-medium', on ? 'border-brand-500 bg-brand-50 text-brand-700' : 'border-line text-gray-600 hover:bg-gray-50')}>
                {label(o.label)}
              </button>
            )
          })}
        </div>
      )
      break
    }
    default:
      input = <Input type={f.field_type === 'email' ? 'email' : f.field_type === 'url' ? 'url' : f.field_type === 'phone' ? 'tel' : 'text'}
        value={(value as string) ?? ''} placeholder={s.placeholder ?? (f.field_type === 'phone' ? '+7 7__ ___-__-__' : '')} invalid={!!error}
        onChange={(e) => onChange(e.target.value)} />
  }
  return <Field label={lbl} required={required} error={error} hint={s.hint}>{input}</Field>
}

// Группировка полей по блокам карточки в порядке сортировки.
export function groupByBlock(fields: CField[], blocks: CFBlock[] = []) {
  const res: { block: CFBlock | null; fields: CField[] }[] = []
  for (const b of [...blocks].sort((a, b) => a.sort_order - b.sort_order)) {
    const fs = fields.filter((f) => f.block_id === b.id)
    if (fs.length) res.push({ block: b, fields: fs })
  }
  const rest = fields.filter((f) => !f.block_id || !blocks.some((b) => b.id === f.block_id))
  if (rest.length) res.push({ block: null, fields: rest })
  return res
}

// Применимо ли поле к записи (область действия: курсы).
export const inScope = (f: CField, courseIds: string[]) => !f.settings.course_ids?.length || courseIds.some((c) => f.settings.course_ids!.includes(c))
