import { useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { api, errorsByField } from '../../lib/api'
import { useAuth } from '../../lib/auth'
import { label } from '../../lib/i18n'
import { dateTime, parseMoney, phone, relative } from '../../lib/format'
import type { CField, Course, Group } from '../../lib/types'
import { Badge, Button, Card, Drawer, Field, Input, Modal, Select, Spinner, StatusBadge, Table, cx, leadStages, paymentMethods } from '../../components/ui'
import { CFInput, cfDisplay, useFields } from '../../components/CustomFields'
import { LeadFormModal, sources } from './LeadForm'

type Lead = { id: string; full_name: string; contact_name: string | null; phone: string; email: string | null; course_id: string | null; course_name: string | null; source: string | null; stage: string; responsible_name: string | null; last_contact_at: string | null; created_at: string; age_or_grade: string | null; preferred_time: string | null; lost_reason: string | null; comment: string | null; student_id: string | null; branch_id: string | null; custom_fields: Record<string, unknown> }

export default function Leads() {
  const { can } = useAuth()
  const [params, setParams] = useSearchParams()
  const stage = params.get('stage') ?? ''
  const [q, setQ] = useState('')
  const [source, setSource] = useState('')
  const [create, setCreate] = useState(false)
  const list = useQuery({ queryKey: ['leads', stage, q, source, params.get('no_contact')], queryFn: () => api.getMeta<Lead[]>(`/leads?stage=${stage}&q=${encodeURIComponent(q)}&source=${encodeURIComponent(source)}${params.get('no_contact') ? '&no_contact=1' : ''}`) })
  const counts = (list.data?.meta.counts ?? {}) as Record<string, number>
  const total = Object.values(counts).reduce((a, b) => a + b, 0)
  const open = params.get('open')
  const setOpen = (id: string | null) => { const n = new URLSearchParams(params); if (id) n.set('open', id); else n.delete('open'); setParams(n) }
  return (
    <div className="mx-auto max-w-[1400px] p-4 lg:p-8">
      <div className="flex items-center justify-between">
        <h1 className="text-[26px] font-bold">Лиды</h1>
        {can('leads.edit') && <Button variant="primary" onClick={() => setCreate(true)}><Plus className="size-4" />Лид</Button>}
      </div>
      {/* воронка: этапы с количеством */}
      <div className="mt-5 grid grid-cols-2 gap-2 sm:grid-cols-4 xl:grid-cols-7">
        {[['', 'Все', total], ...Object.entries(leadStages).map(([k, [l]]) => [k, l, counts[k] ?? 0])].map(([k, l, n]) => (
          <button key={k as string} onClick={() => { const p = new URLSearchParams(); if (k) p.set('stage', k as string); setParams(p) }}
            className={cx('rounded-xl border px-3 py-2.5 text-left', stage === k && !params.get('no_contact') ? 'border-brand-500 bg-brand-50' : 'border-line bg-white hover:border-gray-300')}>
            <div className="text-xs text-muted">{l}</div><div className="text-xl font-bold tabular">{n}</div>
          </button>
        ))}
      </div>
      <div className="mt-4 flex flex-wrap gap-2">
        <Input className="!h-9 max-w-xs" placeholder="Имя или телефон" value={q} onChange={(e) => setQ(e.target.value)} />
        <Select className="!h-9 w-auto" value={source} onChange={(e) => setSource(e.target.value)}><option value="">Все источники</option>{sources.map((s) => <option key={s}>{s}</option>)}</Select>
        {params.get('no_contact') && <Badge tone="red" className="!h-9 !px-3">Без контакта больше 24 ч</Badge>}
      </div>
      <Card className="mt-3" bodyClass="!p-0">
        {list.isLoading ? <Spinner /> : (
          <Table head={['Лид', 'Курс', 'Источник', 'Этап', 'Ответственный', 'Последний контакт', 'Создан']}>
            {list.data?.data.map((l) => {
              const stale = l.stage === 'new' && !l.last_contact_at && Date.now() - new Date(l.created_at).getTime() > 864e5
              return (
                <tr key={l.id} className="cursor-pointer hover:bg-gray-50" onClick={() => setOpen(l.id)}>
                  <td className="px-4 py-3"><div className="font-medium">{l.full_name}</div><div className="text-xs text-muted">{phone(l.phone)}{l.contact_name && ` · ${l.contact_name}`}</div></td>
                  <td className="px-4 py-3 text-[13px]">{l.course_name ?? '—'}</td>
                  <td className="px-4 py-3 text-[13px]">{l.source ?? '—'}</td>
                  <td className="px-4 py-3"><StatusBadge map={leadStages} value={l.stage} /></td>
                  <td className="px-4 py-3 text-[13px]">{l.responsible_name ?? '—'}</td>
                  <td className={cx('px-4 py-3 text-[13px]', stale && 'font-medium text-red-700')}>{l.last_contact_at ? relative(l.last_contact_at) : stale ? 'нет контакта > 24 ч' : '—'}</td>
                  <td className="px-4 py-3 text-[13px] text-muted">{dateTime(l.created_at)}</td>
                </tr>
              )
            })}
          </Table>
        )}
      </Card>
      {create && <LeadFormModal onClose={() => setCreate(false)} />}
      {open && <LeadDrawer id={open} onClose={() => setOpen(null)} />}
    </div>
  )
}

function LeadDrawer({ id, onClose }: { id: string; onClose: () => void }) {
  const { can } = useAuth()
  const qc = useQueryClient()
  const nav = useNavigate()
  const q = useQuery({ queryKey: ['lead', id], queryFn: () => api.get<{ lead: Lead; fields: CField[]; history: { action: string; new: Record<string, string>; created_at: string; user: string }[]; trials: { lesson_id: string; starts_at: string; group: string; status: string | null }[] }>('/leads/' + id) })
  const [convert, setConvert] = useState(false)
  const [lost, setLost] = useState(false)
  const update = useMutation({ mutationFn: (body: object) => api.patch('/leads/' + id, body), onSuccess: () => { qc.invalidateQueries({ queryKey: ['lead', id] }); qc.invalidateQueries({ queryKey: ['leads'] }) } })
  if (!q.data) return <Drawer open title="Лид" onClose={onClose}><Spinner /></Drawer>
  const { lead: l, fields, history, trials } = q.data
  return (
    <Drawer open title={l.full_name} onClose={onClose}>
      <div className="space-y-5 text-sm">
        <div className="flex flex-wrap items-center gap-2"><StatusBadge map={leadStages} value={l.stage} />{l.source && <Badge>{l.source}</Badge>}</div>
        <dl className="grid grid-cols-2 gap-3">
          <div><dt className="text-muted">Телефон</dt><dd><a href={`tel:${l.phone}`} className="text-brand-600">{phone(l.phone)}</a></dd></div>
          <div><dt className="text-muted">Контактное лицо</dt><dd>{l.contact_name ?? '—'}</dd></div>
          <div><dt className="text-muted">Курс</dt><dd>{l.course_name ?? '—'}</dd></div>
          <div><dt className="text-muted">Возраст / класс</dt><dd>{l.age_or_grade ?? '—'}</dd></div>
          <div><dt className="text-muted">Удобное время</dt><dd>{l.preferred_time ?? '—'}</dd></div>
          <div><dt className="text-muted">Ответственный</dt><dd>{l.responsible_name ?? '—'}</dd></div>
          {fields.map((f) => <div key={f.key}><dt className="text-muted">{label(f.label)}</dt><dd>{cfDisplay(f, l.custom_fields[f.key])}</dd></div>)}
          {l.lost_reason && <div className="col-span-2"><dt className="text-muted">Причина отказа</dt><dd>{l.lost_reason}</dd></div>}
          {l.comment && <div className="col-span-2"><dt className="text-muted">Комментарий</dt><dd>{l.comment}</dd></div>}
        </dl>
        {trials.length > 0 && <div><div className="mb-1 font-semibold">Пробные занятия</div>{trials.map((t) => <div key={t.lesson_id}>{t.group} · {dateTime(t.starts_at)} · {t.status ? 'проведено' : 'запланировано'}</div>)}</div>}
        {can('leads.edit') && !l.student_id && (
          <div className="flex flex-wrap gap-2 border-t border-line pt-4">
            {l.stage === 'new' && <Button onClick={() => update.mutate({ stage: 'contacted', contacted: true })}>Связались</Button>}
            <Select className="!h-10 w-auto" value={l.stage} onChange={(e) => e.target.value === 'lost' ? setLost(true) : update.mutate({ stage: e.target.value })}>
              {Object.entries(leadStages).filter(([k]) => k !== 'won').map(([k, [lbl]]) => <option key={k} value={k}>{lbl}</option>)}
            </Select>
            <Button variant="primary" onClick={() => setConvert(true)}>Конвертировать в ученика</Button>
          </div>
        )}
        {l.student_id && <Button onClick={() => nav('/students/' + l.student_id)}>Открыть карточку ученика</Button>}
        <div><div className="mb-1 font-semibold">История</div>{history.map((h, i) => <div key={i} className="py-1 text-[13px]"><span>{h.action === 'stage_changed' ? `Этап: ${leadStages[h.new?.stage]?.[0] ?? h.new?.stage}` : h.action === 'converted' ? 'Конвертирован в ученика' : h.action === 'create' ? 'Создан' : 'Изменён'}</span><span className="text-muted"> · {h.user} · {dateTime(h.created_at)}</span></div>)}</div>
      </div>
      {convert && <ConvertModal lead={l} onClose={() => setConvert(false)} onDone={(sid) => { qc.invalidateQueries(); nav('/students/' + sid) }} />}
      {lost && <LostModal onClose={() => setLost(false)} onSave={(reason) => { update.mutate({ stage: 'lost', lost_reason: reason }); setLost(false) }} />}
    </Drawer>
  )
}

function LostModal({ onClose, onSave }: { onClose: () => void; onSave: (r: string) => void }) {
  const [r, setR] = useState('Дорого')
  return (
    <Modal open title="Причина отказа" onClose={onClose} footer={<><Button onClick={onClose}>Отмена</Button><Button variant="danger" onClick={() => onSave(r)}>Отказ</Button></>}>
      <Select value={r} onChange={(e) => setR(e.target.value)}>{['Дорого', 'Не подошло расписание', 'Выбрали другой центр', 'Не отвечает', 'Передумали'].map((x) => <option key={x}>{x}</option>)}</Select>
    </Modal>
  )
}

// Конвертация одним действием: ученик + родитель + группа + абонемент + первый платёж (ТЗ 5.4).
function ConvertModal({ lead, onClose, onDone }: { lead: Lead; onClose: () => void; onDone: (sid: string) => void }) {
  const { can } = useAuth()
  const groups = useQuery({ queryKey: ['groups', 'all-active'], queryFn: () => api.get<Group[]>('/groups') })
  const courses = useQuery({ queryKey: ['courses'], queryFn: () => api.get<Course[]>('/courses') })
  const fieldsQ = useFields('student')
  const [f, setF] = useState({ student_name: lead.full_name, student_phone: '', parent_name: lead.contact_name?.replace(/\s*\(.*\)/, '') ?? '', parent_phone: lead.phone, group_id: '', sub: true, pay: '', method: 'kaspi' })
  const [errors, setErrors] = useState<Record<string, string>>({})
  const group = groups.data?.find((g) => g.id === f.group_id)
  const course = courses.data?.find((c) => c.id === group?.course_id)
  const required = (fieldsQ.data?.data ?? []).filter((x) => (x.settings.required === 'always' || (x.settings.required === 'on_status' && x.settings.required_status === 'active')) && (!x.settings.course_ids?.length || (group && x.settings.course_ids.includes(group.course_id))))
  const [cf, setCf] = useState<Record<string, unknown>>({})
  const m = useMutation({
    mutationFn: () => api.post<{ student_id: string }>(`/leads/${lead.id}/convert`, {
      ...f, custom_fields: cf, subscription: f.sub && course ? { type: 'lessons', lessons_total: course.lessons_count, price_tiyn: course.price_tiyn } : undefined,
      payment: f.pay && can('payments.create') ? { amount_tiyn: parseMoney(f.pay), method: f.method } : undefined,
    }),
    onSuccess: (r) => onDone(r.student_id),
    onError: (e) => setErrors({ ...errorsByField(e), _: (e as Error).message }),
  })
  return (
    <Modal open wide title="Конвертация в ученика" onClose={onClose} footer={<><Button onClick={onClose}>Отмена</Button><Button variant="primary" loading={m.isPending} onClick={() => m.mutate()}>Создать ученика</Button></>}>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="ФИО ученика"><Input value={f.student_name} onChange={(e) => setF({ ...f, student_name: e.target.value })} /></Field>
        <Field label="Телефон ученика"><Input value={f.student_phone} onChange={(e) => setF({ ...f, student_phone: e.target.value })} /></Field>
        <Field label="Родитель"><Input value={f.parent_name} onChange={(e) => setF({ ...f, parent_name: e.target.value })} /></Field>
        <Field label="Телефон родителя"><Input value={f.parent_phone} onChange={(e) => setF({ ...f, parent_phone: e.target.value })} /></Field>
        <Field label="Группа" className="sm:col-span-2">
          <Select value={f.group_id} onChange={(e) => setF({ ...f, group_id: e.target.value })}>
            <option value="">— без группы —</option>
            {groups.data?.map((g) => <option key={g.id} value={g.id} disabled={!!g.capacity && g.students >= g.capacity}>{g.name} · {g.students}/{g.capacity ?? '∞'}{g.course_id === lead.course_id ? ' ★' : ''}</option>)}
          </Select>
        </Field>
        {required.map((x) => (
          <CFInputLazy key={x.key} f={x} value={cf[x.key]} onChange={(v) => setCf({ ...cf, [x.key]: v })} error={errors['custom_fields.' + x.key]} />
        ))}
        {course && <label className="flex items-center gap-2 text-sm sm:col-span-2"><input type="checkbox" checked={f.sub} onChange={(e) => setF({ ...f, sub: e.target.checked })} />Оформить абонемент «{course.lessons_count} занятий · {course.name}» за {(course.price_tiyn / 100).toLocaleString('ru-RU')} ₸</label>}
        {can('payments.create') && (
          <>
            <Field label="Первый платёж, ₸"><Input value={f.pay} onChange={(e) => setF({ ...f, pay: e.target.value })} placeholder="необязательно" /></Field>
            <Field label="Способ"><Select value={f.method} onChange={(e) => setF({ ...f, method: e.target.value })}>{Object.entries(paymentMethods).map(([k, l]) => <option key={k} value={k}>{l}</option>)}</Select></Field>
          </>
        )}
      </div>
      {errors._ && <div className="mt-3 text-sm text-red-700">{errors._}</div>}
    </Modal>
  )
}

const CFInputLazy = ({ f, value, onChange, error }: { f: CField; value: unknown; onChange: (v: unknown) => void; error?: string }) => <CFInput field={f} value={value} onChange={onChange} error={error} required />
