import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, SlidersHorizontal, X } from 'lucide-react'
import { api, download, request } from '../lib/api'
import { useAuth } from '../lib/auth'
import { label } from '../lib/i18n'
import { date, money, phone } from '../lib/format'
import type { CField, Course, Group, StudentRow } from '../lib/types'
import { Button, Checkbox, Field, Input, Modal, Select, Spinner, StatusBadge, Toast, cx, studentStatus } from '../components/ui'
import { CFInput, cfDisplay, useFields } from '../components/CustomFields'
import { StudentFormModal } from './StudentForm'
import { ImportModal } from './ImportModal'

type View = { id: string; name: string; config: { filters?: Record<string, string>; columns?: string[] }; shared: boolean }
const STD_FILTERS = ['status', 'course_id', 'group_id', 'teacher_id', 'balance', 'subscription', 'q']

export default function Students() {
  const { can, branchId } = useAuth()
  const nav = useNavigate()
  const qc = useQueryClient()
  const [params, setParams] = useSearchParams()
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [modal, setModal] = useState<null | 'create' | 'import' | 'filter' | 'columns' | 'view' | 'bulk-status' | 'bulk-group' | 'bulk-field'>(params.get('import') ? 'import' : null)
  const [toast, setToast] = useState('')
  const fieldsQ = useFields('student')
  const fields = useMemo(() => fieldsQ.data?.data ?? [], [fieldsQ.data])
  const views = useQuery({ queryKey: ['views', 'student'], queryFn: () => api.get<View[]>('/saved-views?entity=student') })
  const courses = useQuery({ queryKey: ['courses'], queryFn: () => api.get<Course[]>('/courses') })
  const activeView = params.get('view') ?? ''
  const [columns, setColumns] = useState<string[] | null>(() => JSON.parse(localStorage.getItem('emp.students.columns') ?? 'null'))
  const visibleCF = fields.filter((f) => (columns ? columns.includes(f.key) : f.settings.show_in_table))

  // фильтры = параметры URL (кроме служебных)
  const filters: Record<string, string> = {}
  params.forEach((v, k) => { if (k !== 'view' && k !== 'import' && v) filters[k] = v })
  const qs = new URLSearchParams({ ...filters, ...(branchId ? { branch_id: branchId } : {}) }).toString()

  const list = useInfiniteQuery({
    queryKey: ['students', qs],
    queryFn: ({ pageParam }) => request<StudentRow[]>('GET', `/students?limit=50&${qs}${pageParam ? '&cursor=' + pageParam : ''}`),
    initialPageParam: '',
    getNextPageParam: (last) => (last.meta.next_cursor as string) || undefined,
  })
  const rows = list.data?.pages.flatMap((p) => p.data) ?? []
  const total = (list.data?.pages[0]?.meta.total as number) ?? 0

  useEffect(() => setSelected(new Set()), [qs])

  const applyView = (v: View | null) => {
    const next = new URLSearchParams()
    if (v) {
      next.set('view', v.id)
      Object.entries(v.config.filters ?? {}).forEach(([k, val]) => next.set(k, val))
      if (v.config.columns) setColumns(v.config.columns)
    }
    setParams(next)
  }
  const setFilter = (k: string, v: string) => {
    const next = new URLSearchParams(params)
    if (v) next.set(k, v)
    else next.delete(k)
    setParams(next)
  }

  const bulk = useMutation({
    mutationFn: (body: { action: string; value?: unknown; field?: string }) => api.post<{ updated: number }>('/students/bulk', { ids: [...selected], ...body }),
    onSuccess: (r) => { setToast(`Обновлено: ${r.updated}`); setSelected(new Set()); setModal(null); qc.invalidateQueries({ queryKey: ['students'] }) },
    onError: (e: Error) => setToast(e.message),
  })

  const cfFilterChips = Object.entries(filters).filter(([k]) => k.startsWith('cf.'))
  const fieldByKey = (k: string) => fields.find((f) => f.key === k.replace(/^cf\./, '').replace(/\[.*\]$/, ''))
  const opText = (k: string) => ({ gte: '≥', lte: '≤', gt: '>', lt: '<', contains: 'содержит', empty: 'пусто' }[/\[(\w+)\]$/.exec(k)?.[1] ?? ''] ?? ':')

  const all = rows.length > 0 && rows.every((r) => selected.has(r.id))
  return (
    <div className="mx-auto max-w-[1500px] p-4 lg:p-8">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-[26px] font-bold">Ученики <span className="ml-1 text-lg font-medium text-muted tabular">{total}</span></h1>
        <div className="flex gap-2">
          {can('students.import') && <Button onClick={() => setModal('import')}>Импорт</Button>}
          {can('students.export') && <Button onClick={() => download(`/students/export?format=xlsx&${qs}`).catch((e) => setToast(e.message))}>Экспорт</Button>}
          {can('students.edit') && <Button variant="primary" onClick={() => setModal('create')}><Plus className="size-4" />Ученик</Button>}
        </div>
      </div>

      {/* «Виды» — сохранённые наборы фильтров и колонок */}
      <div className="mt-5 flex items-center gap-1 overflow-x-auto border-b border-line">
        {[{ id: '', name: 'Все' } as View, ...(views.data ?? [])].map((v) => (
          <button key={v.id} onClick={() => applyView(v.id ? v : null)}
            className={cx('-mb-px whitespace-nowrap border-b-2 px-3 pb-2.5 pt-1 text-sm font-medium', activeView === v.id ? 'border-brand-600 text-ink' : 'border-transparent text-muted hover:text-ink')}>
            {v.name}
          </button>
        ))}
        <button onClick={() => setModal('view')} className="ml-2 whitespace-nowrap px-2 pb-2.5 pt-1 text-sm font-medium text-brand-600 hover:underline">+ Сохранить вид</button>
      </div>

      <div className="mt-4 flex flex-wrap items-center gap-2">
        <Input placeholder="Поиск по ФИО, телефону, ID" className="!h-9 w-60" defaultValue={filters.q ?? ''}
          onKeyDown={(e) => e.key === 'Enter' && setFilter('q', (e.target as HTMLInputElement).value)} onBlur={(e) => e.target.value !== (filters.q ?? '') && setFilter('q', e.target.value)} />
        <Select className="!h-9 w-auto" value={filters.status ?? ''} onChange={(e) => setFilter('status', e.target.value)}>
          <option value="">Статус: все</option>
          {Object.entries(studentStatus).map(([k, [l]]) => <option key={k} value={k}>Статус: {l}</option>)}
        </Select>
        <Select className="!h-9 w-auto max-w-60" value={filters.course_id ?? ''} onChange={(e) => setFilter('course_id', e.target.value)}>
          <option value="">Курс: все</option>
          {courses.data?.map((c) => <option key={c.id} value={c.id}>Курс: {c.name}</option>)}
        </Select>
        {can('finance.view') && (
          <Select className="!h-9 w-auto" value={filters.balance ?? ''} onChange={(e) => setFilter('balance', e.target.value)}>
            <option value="">Баланс: любой</option><option value="negative">Должники</option><option value="positive">Положительный</option>
          </Select>
        )}
        {filters.subscription && <Chip onRemove={() => setFilter('subscription', '')}>Абонемент заканчивается</Chip>}
        {cfFilterChips.map(([k, v]) => {
          const f = fieldByKey(k)
          if (!f) return null
          const shown = f.field_type === 'select' || f.field_type === 'multiselect' ? v.split(',').map((x) => label(f.options.find((o) => o.id === x || o.label.ru === x)?.label) || x).join(', ') : v
          return <Chip key={k} blue onRemove={() => setFilter(k, '')}>{label(f.label)} {opText(k)} {opText(k) === 'пусто' ? '' : shown}</Chip>
        })}
        <button onClick={() => setModal('filter')} className="h-9 rounded-lg border border-dashed border-gray-300 px-3 text-sm text-muted hover:bg-gray-50">+ Фильтр</button>
        {Object.keys(filters).length > 0 && <button onClick={() => applyView(null)} className="px-2 text-sm text-muted hover:text-ink">Сбросить</button>}
        <div className="flex-1" />
        <Button size="sm" onClick={() => setModal('columns')}><SlidersHorizontal className="size-3.5" />Колонки</Button>
      </div>

      {selected.size > 0 && (
        <div className="mt-3 flex flex-wrap items-center gap-2 rounded-xl bg-side px-4 py-2.5 text-sm text-white">
          <span className="mr-2 font-semibold">Выбрано: {selected.size}</span>
          <Button size="sm" variant="dark" onClick={() => setModal('bulk-status')}>Изменить статус</Button>
          <Button size="sm" variant="dark" onClick={() => setModal('bulk-group')}>Перевести в группу</Button>
          <Button size="sm" variant="dark" onClick={() => setModal('bulk-field')}>Изменить поле</Button>
          <Button size="sm" variant="dark" disabled title="Массовые рассылки (WhatsApp/SMS) — этап 7 roadmap">Написать родителям</Button>
          <div className="flex-1" />
          <button onClick={() => setSelected(new Set())} className="text-gray-300 hover:text-white">Снять выбор</button>
        </div>
      )}

      <div className="mt-3 overflow-hidden rounded-xl border border-line bg-white">
        {list.isLoading ? <Spinner /> : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-line text-left text-[11px] font-semibold uppercase tracking-wide text-muted">
                  <th className="w-10 px-4 py-3"><Checkbox checked={all} onChange={(v) => setSelected(v ? new Set(rows.map((r) => r.id)) : new Set())} /></th>
                  <th className="px-3 py-3">Ученик</th><th className="px-3 py-3">Статус</th><th className="px-3 py-3">Группа</th>
                  {can('finance.view') && <th className="px-3 py-3 text-right">Баланс</th>}
                  <th className="px-3 py-3">Абонемент</th>
                  {visibleCF.map((f) => <th key={f.key} className="whitespace-nowrap bg-brand-50/60 px-3 py-3 text-brand-700">{label(f.label)}</th>)}
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {rows.map((s) => (
                  <tr key={s.id} className={cx('cursor-pointer hover:bg-gray-50', selected.has(s.id) && 'bg-brand-50/40')} onClick={() => nav('/students/' + s.id)}>
                    <td className="px-4 py-3" onClick={(e) => e.stopPropagation()}>
                      <Checkbox checked={selected.has(s.id)} onChange={(v) => { const n = new Set(selected); if (v) n.add(s.id); else n.delete(s.id); setSelected(n) }} />
                    </td>
                    <td className="px-3 py-3"><div className="font-medium">{s.full_name}</div><div className="text-xs text-muted tabular">{phone(s.phone)}</div></td>
                    <td className="px-3 py-3"><StatusBadge map={studentStatus} value={s.status} /></td>
                    <td className="px-3 py-3 text-[13px]">{s.groups.length ? s.groups.map((g) => g.name).join(', ') : <span className="text-muted">—</span>}</td>
                    {can('finance.view') && <td className={cx('whitespace-nowrap px-3 py-3 text-right font-semibold tabular', (s.balance_tiyn ?? 0) < 0 && 'text-red-700')}>{money(s.balance_tiyn)}</td>}
                    <td className="whitespace-nowrap px-3 py-3 text-[13px]"><SubText s={s} /></td>
                    {visibleCF.map((f) => <td key={f.key} className="px-3 py-3 text-[13px]">{cfDisplay(f, s.custom_fields[f.key])}</td>)}
                  </tr>
                ))}
              </tbody>
            </table>
            {rows.length === 0 && <div className="py-14 text-center text-sm text-muted">Никого не найдено. Измените фильтры или <button className="text-brand-600" onClick={() => setModal('import')}>импортируйте учеников из Excel</button>.</div>}
          </div>
        )}
      </div>
      <div className="mt-3 flex items-center justify-between text-[13px] text-muted">
        <span>Показано {rows.length} из {total}{Object.keys(filters).length ? ' по фильтру' : ''} · синим отмечены пользовательские поля</span>
        {list.hasNextPage && <Button size="sm" loading={list.isFetchingNextPage} onClick={() => list.fetchNextPage()}>Показать ещё</Button>}
      </div>

      {modal === 'create' && <StudentFormModal onClose={() => setModal(null)} onCreated={(id) => nav('/students/' + id)} />}
      {modal === 'import' && <ImportModal fields={fields} onClose={() => { setModal(null); qc.invalidateQueries({ queryKey: ['students'] }) }} />}
      {modal === 'filter' && <FilterModal fields={fields} onClose={() => setModal(null)} onApply={(k, v) => { setFilter(k, v); setModal(null) }} />}
      {modal === 'columns' && (
        <Modal open title="Колонки таблицы" onClose={() => setModal(null)} footer={<Button variant="primary" onClick={() => setModal(null)}>Готово</Button>}>
          <div className="space-y-2.5">
            {fields.map((f) => (
              <Checkbox key={f.key} checked={visibleCF.some((x) => x.key === f.key)} label={label(f.label)} onChange={(v) => {
                const next = v ? [...visibleCF.map((x) => x.key), f.key] : visibleCF.map((x) => x.key).filter((k) => k !== f.key)
                setColumns(next)
                localStorage.setItem('emp.students.columns', JSON.stringify(next))
              }} />
            ))}
          </div>
        </Modal>
      )}
      {modal === 'view' && <SaveViewModal filters={filters} columns={visibleCF.map((f) => f.key)} onClose={() => setModal(null)} onSaved={(id) => { qc.invalidateQueries({ queryKey: ['views'] }); setParams({ ...filters, view: id }); setModal(null) }} />}
      {modal === 'bulk-status' && <BulkStatus onClose={() => setModal(null)} onApply={(v) => bulk.mutate({ action: 'status', value: v })} busy={bulk.isPending} />}
      {modal === 'bulk-group' && <BulkGroup onClose={() => setModal(null)} onApply={(v) => bulk.mutate({ action: 'group', value: v })} busy={bulk.isPending} />}
      {modal === 'bulk-field' && <BulkField fields={fields} onClose={() => setModal(null)} onApply={(field, value) => bulk.mutate({ action: 'field', field, value })} busy={bulk.isPending} />}
      {toast && <Toast text={toast} onDone={() => setToast('')} />}
    </div>
  )
}

function SubText({ s }: { s: StudentRow }) {
  const sub = s.subscription
  if (!sub) return <span className="text-muted">не оформлен</span>
  if (sub.frozen_until) return <span className="text-indigo-700">заморожен до {date(sub.frozen_until).slice(0, 5)}</span>
  if (sub.lessons_total) {
    const left = sub.lessons_total - sub.lessons_used
    return <span className={cx(left <= 2 && 'font-medium text-red-700')}>{left} из {sub.lessons_total}</span>
  }
  return <span>{sub.name}{sub.ends_on ? ` до ${date(sub.ends_on).slice(0, 5)}` : ''}</span>
}

function Chip({ children, onRemove, blue }: { children: React.ReactNode; onRemove: () => void; blue?: boolean }) {
  return (
    <span className={cx('inline-flex h-9 items-center gap-1.5 rounded-lg border px-3 text-sm', blue ? 'border-brand-200 bg-brand-50 text-brand-700' : 'border-line bg-white')}>
      {children}<button onClick={onRemove} aria-label="Убрать фильтр"><X className="size-3.5" /></button>
    </span>
  )
}

function FilterModal({ fields, onClose, onApply }: { fields: CField[]; onClose: () => void; onApply: (k: string, v: string) => void }) {
  const [key, setKey] = useState(fields[0]?.key ?? '')
  const f = fields.find((x) => x.key === key)
  const numeric = f && ['integer', 'decimal', 'money'].includes(f.field_type)
  const [op, setOp] = useState('eq')
  const [value, setValue] = useState('')
  const [multi, setMulti] = useState<string[]>([])
  useEffect(() => { setOp(f && ['string', 'text'].includes(f.field_type) ? 'contains' : 'eq'); setValue(''); setMulti([]) }, [key, f])
  const apply = () => {
    if (!f) return
    const k = 'cf.' + f.key + (op === 'eq' ? '' : `[${op}]`)
    let v = value
    if (f.field_type === 'select' || f.field_type === 'multiselect') v = multi.join(',')
    if (op === 'empty') v = '1'
    if (f.field_type === 'money' && v) v = String(Number(v) * 100)
    onApply(k, v)
  }
  return (
    <Modal open title="Фильтр по полю" onClose={onClose} footer={<><Button onClick={onClose}>Отмена</Button><Button variant="primary" onClick={apply}>Применить</Button></>}>
      <div className="space-y-4">
        <Field label="Поле"><Select value={key} onChange={(e) => setKey(e.target.value)}>{fields.map((x) => <option key={x.key} value={x.key}>{label(x.label)}</option>)}</Select></Field>
        {f && (
          <Field label="Условие">
            <Select value={op} onChange={(e) => setOp(e.target.value)}>
              {numeric ? <><option value="eq">равно</option><option value="gte">не меньше</option><option value="lte">не больше</option></> :
                f.field_type === 'date' ? <><option value="gte">после (включительно)</option><option value="lte">до (включительно)</option></> :
                ['string', 'text', 'phone', 'email'].includes(f.field_type) ? <><option value="contains">содержит</option><option value="eq">равно</option></> :
                f.field_type === 'multiselect' ? <><option value="any">любой из</option><option value="all">все из</option></> :
                <option value="eq">{f.field_type === 'select' ? 'один из' : 'равно'}</option>}
              <option value="empty">пусто</option>
            </Select>
          </Field>
        )}
        {f && op !== 'empty' && (
          f.field_type === 'select' || f.field_type === 'multiselect' ? (
            <div className="flex flex-wrap gap-1.5">
              {f.options.map((o) => <button key={o.id} onClick={() => setMulti(multi.includes(o.id) ? multi.filter((x) => x !== o.id) : [...multi, o.id])}
                className={cx('rounded-full border px-2.5 py-1 text-xs font-medium', multi.includes(o.id) ? 'border-brand-500 bg-brand-50 text-brand-700' : 'border-line')}>{label(o.label)}</button>)}
            </div>
          ) : f.field_type === 'boolean' ? (
            <Select value={value} onChange={(e) => setValue(e.target.value)}><option value="">—</option><option value="true">Да</option><option value="false">Нет</option></Select>
          ) : <Field label="Значение"><Input type={f.field_type === 'date' ? 'date' : numeric ? 'number' : 'text'} value={value} onChange={(e) => setValue(e.target.value)} /></Field>
        )}
      </div>
    </Modal>
  )
}

function SaveViewModal({ filters, columns, onClose, onSaved }: { filters: Record<string, string>; columns: string[]; onClose: () => void; onSaved: (id: string) => void }) {
  const { can } = useAuth()
  const [name, setName] = useState('')
  const [shared, setShared] = useState(false)
  const m = useMutation({ mutationFn: () => api.post<{ id: string }>('/saved-views', { entity_type: 'student', name, shared, config: { filters, columns } }), onSuccess: (r) => onSaved(r.id) })
  return (
    <Modal open title="Сохранить вид" onClose={onClose} footer={<><Button onClick={onClose}>Отмена</Button><Button variant="primary" loading={m.isPending} disabled={!name.trim()} onClick={() => m.mutate()}>Сохранить</Button></>}>
      <Field label="Название"><Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Например, ЕНТ · 11 класс" /></Field>
      <div className="mt-2 text-xs text-muted">Сохранятся текущие фильтры ({Object.keys(filters).filter((k) => STD_FILTERS.includes(k) || k.startsWith('cf.')).length}) и колонки ({columns.length}).</div>
      {can('settings.manage') && <div className="mt-4"><Checkbox checked={shared} onChange={setShared} label="Общий вид для всей организации" /></div>}
    </Modal>
  )
}

function BulkStatus({ onClose, onApply, busy }: { onClose: () => void; onApply: (v: string) => void; busy: boolean }) {
  const [v, setV] = useState('active')
  return (
    <Modal open title="Изменить статус" onClose={onClose} footer={<><Button onClick={onClose}>Отмена</Button><Button variant="primary" loading={busy} onClick={() => onApply(v)}>Применить</Button></>}>
      <Select value={v} onChange={(e) => setV(e.target.value)}>{Object.entries(studentStatus).map(([k, [l]]) => <option key={k} value={k}>{l}</option>)}</Select>
    </Modal>
  )
}

function BulkGroup({ onClose, onApply, busy }: { onClose: () => void; onApply: (v: string) => void; busy: boolean }) {
  const groups = useQuery({ queryKey: ['groups', 'all-active'], queryFn: () => api.get<Group[]>('/groups') })
  const [v, setV] = useState('')
  return (
    <Modal open title="Перевести в группу" onClose={onClose} footer={<><Button onClick={onClose}>Отмена</Button><Button variant="primary" disabled={!v} loading={busy} onClick={() => onApply(v)}>Перевести</Button></>}>
      <Select value={v} onChange={(e) => setV(e.target.value)}>
        <option value="">Выберите группу</option>
        {groups.data?.map((g) => <option key={g.id} value={g.id}>{g.name} · {g.students}/{g.capacity ?? '∞'}</option>)}
      </Select>
      <p className="mt-2 text-xs text-muted">Ученики выйдут из текущих групп того же курса (причина — «перевод»), история членства сохранится.</p>
    </Modal>
  )
}

function BulkField({ fields, onClose, onApply, busy }: { fields: CField[]; onClose: () => void; onApply: (k: string, v: unknown) => void; busy: boolean }) {
  const [key, setKey] = useState(fields[0]?.key ?? '')
  const [value, setValue] = useState<unknown>(null)
  const f = fields.find((x) => x.key === key)
  return (
    <Modal open title="Изменить поле" onClose={onClose} footer={<><Button onClick={onClose}>Отмена</Button><Button variant="primary" loading={busy} onClick={() => onApply(key, value)}>Применить</Button></>}>
      <Field label="Поле"><Select value={key} onChange={(e) => { setKey(e.target.value); setValue(null) }}>{fields.map((x) => <option key={x.key} value={x.key}>{label(x.label)}</option>)}</Select></Field>
      {f && <div className="mt-4"><BulkInput f={f} value={value} onChange={setValue} /></div>}
    </Modal>
  )
}

const BulkInput = ({ f, value, onChange }: { f: CField; value: unknown; onChange: (v: unknown) => void }) => <CFInput field={f} value={value} onChange={onChange} />
