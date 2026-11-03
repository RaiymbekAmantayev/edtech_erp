import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { GripVertical, Plus } from 'lucide-react'
import { api, errorsByField } from '../../lib/api'
import { label } from '../../lib/i18n'
import type { CField, CFSettings, Course } from '../../lib/types'
import { Badge, Button, Checkbox, Field, Input, Modal, Select, Spinner, Toast, cx } from '../../components/ui'
import { CFInput, fieldTypes, groupByBlock, typeLabel, useBlocks, useFields } from '../../components/CustomFields'

const entities = [['student', 'Ученик'], ['parent', 'Родитель'], ['lead', 'Лид'], ['employee', 'Преподаватель'], ['group', 'Группа'], ['payment', 'Платёж'], ['course', 'Курс']]
const roles = [['org_admin', 'Администратор'], ['branch_manager', 'Управляющий филиалом'], ['teacher', 'Преподаватель'], ['sales', 'Менеджер продаж'], ['accountant', 'Бухгалтер'], ['academic', 'Учебный отдел']]
const mainTypes = ['string', 'text', 'integer', 'money', 'date', 'boolean', 'select', 'multiselect', 'phone', 'url', 'file', 'employee']

function describe(f: CField) {
  const s = f.settings
  return [s.required === 'always' ? 'обязательное' : s.required === 'on_status' ? 'обязательное при статусе' : 'необязательное', f.is_unique && 'уникальное', f.is_sensitive && 'чувствительное',
    s.course_ids?.length && 'только выбранные курсы', s.show_in_table && 'в таблице'].filter(Boolean).join(' · ')
}

// Настройки → Пользовательские поля (ТЗ 9, макет). Поля сразу появляются в карточках, таблицах, фильтрах, импорте, экспорте и API.
export default function CustomFields() {
  const [entity, setEntity] = useState('student')
  const [sel, setSel] = useState<string | 'new' | null>(null)
  const [blockModal, setBlockModal] = useState(false)
  const [showArchived, setShowArchived] = useState(false)
  const fq = useFields(entity, true)
  const blocks = useBlocks(entity)
  const all = fq.data?.data ?? []
  const active = all.filter((f) => !f.archived_at)
  const meta = fq.data?.meta as { limit_per_entity: number; used: number; used_total: number; counts: Record<string, number> } | undefined
  const current = all.find((f) => f.id === sel)
  useEffect(() => { if (!sel && active.length) setSel(active[0].id) }, [active, sel])
  useEffect(() => setSel(null), [entity])

  return (
    <div className="max-w-[1250px]">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-[26px] font-bold">Пользовательские поля</h1>
          <p className="mt-1 max-w-2xl text-sm text-muted">Поля появятся в карточке, таблице, фильтрах, импорте, экспорте, шаблонах документов и API. Использовано {meta?.used ?? 0} из {meta?.limit_per_entity ?? '—'} полей для этой сущности на текущем тарифе.</p>
        </div>
        <div className="flex gap-2"><Button onClick={() => setBlockModal(true)}>+ Блок</Button><Button variant="primary" onClick={() => setSel('new')}><Plus className="size-4" />Поле</Button></div>
      </div>
      <div className="mt-5 inline-flex flex-wrap gap-1 rounded-xl bg-gray-100 p-1">
        {entities.map(([k, l]) => (
          <button key={k} onClick={() => setEntity(k)} className={cx('rounded-lg px-3.5 py-1.5 text-sm font-medium', entity === k ? 'bg-white shadow-sm' : 'text-muted hover:text-ink')}>
            {l} <span className="text-xs text-muted">{meta?.counts?.[k] ?? 0}</span>
          </button>
        ))}
      </div>

      <div className="mt-5 grid gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.15fr)]">
        <div className="space-y-4">
          {fq.isLoading ? <Spinner /> : groupByBlock(active, blocks.data).map(({ block, fields }) => (
            <div key={block?.id ?? 'none'} className="overflow-hidden rounded-xl border border-line bg-white">
              <div className="flex items-center justify-between bg-gray-50 px-4 py-3"><span className="text-sm font-semibold">{block ? label(block.name) : 'Без блока'}</span><span className="text-xs text-muted">{fields.length} полей</span></div>
              {fields.map((f) => (
                <button key={f.id} onClick={() => setSel(f.id)} className={cx('flex w-full items-center gap-2 border-t border-line px-4 py-3 text-left', sel === f.id ? 'border-l-4 border-l-brand-600 bg-brand-50/50' : 'hover:bg-gray-50')}>
                  <GripVertical className="size-4 shrink-0 text-gray-300" />
                  <span className="min-w-0 flex-1"><span className="block text-sm font-medium">{label(f.label)}</span><span className="block truncate text-xs text-muted">{describe(f)}</span></span>
                  <Badge className="!rounded-md !font-medium">{typeLabel(f.field_type)}</Badge>
                </button>
              ))}
            </div>
          ))}
          {active.length === 0 && !fq.isLoading && <div className="rounded-xl border border-dashed border-line p-8 text-center text-sm text-muted">Полей пока нет. Нажмите «+ Поле».</div>}
          {all.some((f) => f.archived_at) && (
            <div>
              <button className="text-sm text-muted hover:text-ink" onClick={() => setShowArchived(!showArchived)}>Архив ({all.filter((f) => f.archived_at).length}) {showArchived ? '▴' : '▾'}</button>
              {showArchived && all.filter((f) => f.archived_at).map((f) => <button key={f.id} onClick={() => setSel(f.id)} className="block py-1 text-sm text-muted line-through">{label(f.label)}</button>)}
            </div>
          )}
        </div>
        {(sel === 'new' || current) && <FieldEditor key={sel ?? ''} entity={entity} field={sel === 'new' ? undefined : current} onSaved={(id) => setSel(id)} />}
      </div>
      {blockModal && <BlockModal entity={entity} onClose={() => setBlockModal(false)} />}
    </div>
  )
}

function FieldEditor({ entity, field, onSaved }: { entity: string; field?: CField; onSaved: (id: string) => void }) {
  const qc = useQueryClient()
  const blocks = useBlocks(entity)
  const courses = useQuery({ queryKey: ['courses'], queryFn: () => api.get<Course[]>('/courses'), enabled: entity === 'student' })
  const [lang, setLang] = useState<'ru' | 'kk' | 'en'>('ru')
  const [labels, setLabels] = useState<Record<string, string>>(field?.label ?? { ru: '' })
  const [type, setType] = useState(field?.field_type ?? 'string')
  const [blockId, setBlockId] = useState(field?.block_id ?? '')
  const [s, setS] = useState<CFSettings>(field?.settings ?? { show_in_create: true })
  const [unique, setUnique] = useState(field?.is_unique ?? false)
  const [sensitive, setSensitive] = useState(field?.is_sensitive ?? false)
  const [options, setOptions] = useState<string[]>(field ? [] : ['', ''])
  const [newOption, setNewOption] = useState('')
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [preview, setPreview] = useState(false)
  const [toast, setToast] = useState('')
  const invalidate = () => { qc.invalidateQueries({ queryKey: ['custom-fields'] }); qc.invalidateQueries({ queryKey: ['students'] }) }
  const save = useMutation({
    mutationFn: async () => {
      const body = { entity_type: entity, label: labels, field_type: type, block_id: blockId || (field ? '' : null), settings: s, is_unique: unique, is_sensitive: sensitive }
      if (field) { await api.patch(`/custom-fields/${field.id}`, body); return field.id }
      const r = await api.post<{ id: string }>('/custom-fields', { ...body, block_id: blockId || null, options: options.filter((o) => o.trim()).map((o) => ({ ru: o.trim() })) })
      return r.id
    },
    onSuccess: (id) => { invalidate(); setToast('Сохранено'); onSaved(id) },
    onError: (e) => setErrors({ ...errorsByField(e), _: (e as Error).message }),
  })
  const archive = useMutation({ mutationFn: () => api.post(`/custom-fields/${field!.id}/${field!.archived_at ? 'restore' : 'archive'}`), onSuccess: invalidate })
  const addOpt = useMutation({ mutationFn: () => api.post(`/custom-fields/${field!.id}/options`, { label: { ru: newOption } }), onSuccess: () => { setNewOption(''); invalidate() } })
  const archiveOpt = useMutation({ mutationFn: (o: { id: string; archived: boolean }) => api.patch(`/custom-field-options/${o.id}`, { archived: o.archived }), onSuccess: invalidate })
  const roleList = (k: 'roles_view' | 'roles_edit') => s[k] ?? []
  const toggleRole = (k: 'roles_view' | 'roles_edit', role: string, on: boolean) => {
    const base = s[k]?.length ? s[k]! : roles.map((r) => r[0])
    let next = on ? [...new Set([...base, role])] : base.filter((r) => r !== role)
    if (next.length === roles.length) next = []
    setS({ ...s, [k]: next })
  }
  const canChangeType = !field || (field.field_type === 'string' && type !== 'text') || (field.field_type === 'select')
  const typeAllowed = (t: string) => !field || t === field.field_type || (field.field_type === 'string' && t === 'text') || (field.field_type === 'select' && t === 'multiselect')
  const hasRole = (k: 'roles_view' | 'roles_edit', r: string) => !roleList(k).length || roleList(k).includes(r)

  return (
    <div className="h-fit rounded-xl border border-line bg-white p-5">
      <div className="flex items-start justify-between gap-3">
        <h2 className="text-lg font-semibold">{labels.ru || 'Новое поле'}</h2>
        {field && <code className="rounded bg-gray-50 px-2 py-0.5 text-xs text-muted">key: {field.key}</code>}
      </div>
      <div className="mt-4">
        <div className="mb-1.5 flex items-center gap-2 text-[13px] font-medium">Название
          {(['ru', 'kk', 'en'] as const).map((l) => <button key={l} onClick={() => setLang(l)} className={cx('rounded px-1.5 py-0.5 text-[11px] font-semibold', lang === l ? 'bg-ink text-white' : 'bg-gray-100 text-muted')}>{l === 'kk' ? 'KZ' : l.toUpperCase()}</button>)}
        </div>
        <Input value={labels[lang] ?? ''} onChange={(e) => setLabels({ ...labels, [lang]: e.target.value })} placeholder={lang === 'ru' ? 'Например, Целевой балл ЕНТ' : labels.ru} invalid={!!errors['label.ru']} />
        {errors['label.ru'] && <div className="mt-1 text-xs text-red-600">Укажите название на русском</div>}
      </div>
      <div className="mt-4">
        <div className="mb-1.5 text-[13px] font-medium">Тип поля <span className="font-normal text-muted">· после создания не меняется</span></div>
        <div className="grid grid-cols-4 gap-1.5">
          {mainTypes.map((t) => (
            <button key={t} disabled={!typeAllowed(t)} onClick={() => canChangeType && setType(t)}
              className={cx('h-9 rounded-lg border text-[13px]', type === t ? 'border-brand-500 bg-brand-50 font-semibold text-brand-700' : 'border-line', !typeAllowed(t) && 'opacity-40')}>
              {typeLabel(t)}
            </button>
          ))}
        </div>
        {!mainTypes.includes(type) && <div className="mt-1 text-xs text-muted">{fieldTypes.find((x) => x.value === type)?.label}</div>}
      </div>
      {(type === 'select' || type === 'multiselect') && (
        <div className="mt-4">
          <div className="mb-1.5 text-[13px] font-medium">Варианты</div>
          {field ? (
            <div className="space-y-1">
              {field.options.map((o) => (
                <div key={o.id} className="flex items-center justify-between rounded-lg border border-line px-3 py-1.5 text-sm">
                  <span className={o.archived_at ? 'text-muted line-through' : ''}>{label(o.label)}</span>
                  <button className="text-xs text-muted hover:text-ink" onClick={() => archiveOpt.mutate({ id: o.id, archived: !o.archived_at })}>{o.archived_at ? 'вернуть' : 'в архив'}</button>
                </div>
              ))}
              <div className="flex gap-2"><Input className="!h-9" value={newOption} onChange={(e) => setNewOption(e.target.value)} placeholder="Новый вариант" /><Button size="sm" className="!h-9" disabled={!newOption.trim()} onClick={() => addOpt.mutate()}>Добавить</Button></div>
              <p className="text-xs text-muted">Варианты хранятся по ID: переименование не ломает данные, архивный вариант остаётся у старых записей.</p>
            </div>
          ) : (
            <div className="space-y-1.5">
              {options.map((o, i) => <Input key={i} className="!h-9" value={o} onChange={(e) => setOptions(options.map((x, j) => (j === i ? e.target.value : x)))} placeholder={`Вариант ${i + 1}`} />)}
              <button className="text-sm text-brand-600" onClick={() => setOptions([...options, ''])}>+ вариант</button>
            </div>
          )}
        </div>
      )}
      {(type === 'integer' || type === 'decimal') && (
        <div className="mt-4 grid grid-cols-2 gap-3">
          <Field label="Минимум"><Input type="number" value={s.min ?? ''} onChange={(e) => setS({ ...s, min: e.target.value === '' ? undefined : Number(e.target.value) })} /></Field>
          <Field label="Максимум"><Input type="number" value={s.max ?? ''} onChange={(e) => setS({ ...s, max: e.target.value === '' ? undefined : Number(e.target.value) })} /></Field>
        </div>
      )}
      <div className="mt-4 grid gap-3 sm:grid-cols-2">
        <Field label="Блок в карточке">
          <Select value={blockId} onChange={(e) => setBlockId(e.target.value)}><option value="">Без блока</option>{blocks.data?.map((b) => <option key={b.id} value={b.id}>{label(b.name)}</option>)}</Select>
        </Field>
        <Field label="Обязательность">
          <Select value={s.required === 'on_status' ? 'on_status:' + (s.required_status ?? 'active') : s.required ?? ''} onChange={(e) => {
            const v = e.target.value
            setS(v.startsWith('on_status') ? { ...s, required: 'on_status', required_status: v.split(':')[1] } : { ...s, required: v as CFSettings['required'], required_status: undefined })
          }}>
            <option value="">Не обязательно</option><option value="always">Обязательно всегда</option>
            {entity === 'student' && <option value="on_status:active">При статусе «Занимается»</option>}
          </Select>
        </Field>
      </div>
      {entity === 'student' && (
        <Field className="mt-3" label="Где показывать">
          <Select value={s.course_ids?.length ? 'courses' : 'all'} onChange={(e) => setS({ ...s, course_ids: e.target.value === 'all' ? [] : courses.data?.slice(0, 1).map((c) => c.id) })}>
            <option value="all">Все курсы</option><option value="courses">Только выбранные курсы{s.course_ids?.length ? `: ${courses.data?.filter((c) => s.course_ids!.includes(c.id)).map((c) => c.name).join(', ')}` : ''}</option>
          </Select>
          {!!s.course_ids?.length && (
            <div className="mt-2 flex flex-wrap gap-1.5">
              {courses.data?.map((c) => <button key={c.id} type="button" onClick={() => setS({ ...s, course_ids: s.course_ids!.includes(c.id) ? s.course_ids!.filter((x) => x !== c.id) : [...s.course_ids!, c.id] })}
                className={cx('rounded-full border px-2.5 py-1 text-xs', s.course_ids!.includes(c.id) ? 'border-brand-500 bg-brand-50 text-brand-700' : 'border-line')}>{c.name}</button>)}
            </div>
          )}
        </Field>
      )}
      <div className="mt-4">
        <div className="mb-1.5 text-[13px] font-medium">Доступ по ролям <span className="font-normal text-muted">· владелец видит всё</span></div>
        <div className="overflow-hidden rounded-lg border border-line text-sm">
          <div className="grid grid-cols-[1fr_70px_70px] bg-gray-50 px-3 py-2 text-xs font-semibold text-muted"><span>Роль</span><span className="text-center">Видит</span><span className="text-center">Меняет</span></div>
          {roles.map(([r, l]) => (
            <div key={r} className="grid grid-cols-[1fr_70px_70px] items-center border-t border-line px-3 py-2">
              <span>{l}</span>
              <span className="flex justify-center"><input type="checkbox" className="size-4 accent-brand-600" checked={hasRole('roles_view', r)} onChange={(e) => toggleRole('roles_view', r, e.target.checked)} /></span>
              <span className="flex justify-center"><input type="checkbox" className="size-4 accent-brand-600" checked={hasRole('roles_edit', r) && hasRole('roles_view', r)} onChange={(e) => toggleRole('roles_edit', r, e.target.checked)} /></span>
            </div>
          ))}
        </div>
      </div>
      <div className="mt-4">
        <div className="mb-2 text-[13px] font-medium">Отображение</div>
        <div className="grid gap-2 sm:grid-cols-2">
          <Checkbox checked={!!s.show_in_create} onChange={(v) => setS({ ...s, show_in_create: v })} label="В форме создания" />
          <Checkbox checked={!!s.show_in_table} onChange={(v) => setS({ ...s, show_in_table: v })} label="Колонкой в таблице" />
          <Checkbox checked={!!s.show_in_teacher} onChange={(v) => setS({ ...s, show_in_teacher: v })} label="В кабинете преподавателя" />
          <Checkbox checked={!!s.show_in_parent} onChange={(v) => setS({ ...s, show_in_parent: v })} label="В портале родителя" />
          <Checkbox checked={unique} onChange={setUnique} label="Уникальное значение" />
          <Checkbox checked={sensitive} onChange={setSensitive} label="Чувствительное (маска)" />
        </div>
      </div>
      {errors._ && !errors['label.ru'] && <div className="mt-3 text-sm text-red-700">{errors._}</div>}
      <div className="mt-5 flex flex-wrap items-center gap-2 border-t border-line pt-4">
        <Button variant="primary" loading={save.isPending} onClick={() => { setErrors({}); save.mutate() }}>Сохранить</Button>
        <Button onClick={() => setPreview(true)}>Предпросмотр формы</Button>
        <div className="flex-1" />
        {field && <button className="text-sm text-red-700 hover:underline" onClick={() => (field.archived_at || confirm('Отправить поле в архив? Данные сохранятся, поле скроется из форм и таблиц.')) && archive.mutate()}>{field.archived_at ? 'Вернуть из архива' : 'В архив'}</button>}
      </div>
      {preview && (
        <Modal open title="Предпросмотр: как увидит администратор" onClose={() => setPreview(false)}>
          <CFInput field={{ ...(field ?? { id: 'x', entity_type: entity, key: 'preview', block_id: null, sort_order: 0, archived_at: null, options: options.filter(Boolean).map((o, i) => ({ id: String(i), label: { ru: o }, color: null, sort_order: i, archived_at: null })) }), label: labels, field_type: type, settings: s, is_sensitive: sensitive, is_unique: unique } as CField}
            value={undefined} onChange={() => {}} required={s.required === 'always'} />
          <p className="mt-4 text-xs text-muted">{s.show_in_teacher ? 'Преподаватель увидит поле в списке отметки посещаемости.' : 'Преподаватель не увидит это поле в своём кабинете.'}</p>
        </Modal>
      )}
      {toast && <Toast text={toast} onDone={() => setToast('')} />}
    </div>
  )
}

function BlockModal({ entity, onClose }: { entity: string; onClose: () => void }) {
  const qc = useQueryClient()
  const [name, setName] = useState('')
  const m = useMutation({ mutationFn: () => api.post('/custom-field-blocks', { entity_type: entity, name: { ru: name } }), onSuccess: () => { qc.invalidateQueries({ queryKey: ['cf-blocks'] }); onClose() } })
  return (
    <Modal open title="Новый блок полей" onClose={onClose} footer={<><Button onClick={onClose}>Отмена</Button><Button variant="primary" disabled={!name.trim()} loading={m.isPending} onClick={() => m.mutate()}>Создать</Button></>}>
      <Field label="Название блока" hint="Например: Анкета, Медицинское, Для договора"><Input autoFocus value={name} onChange={(e) => setName(e.target.value)} /></Field>
    </Modal>
  )
}
