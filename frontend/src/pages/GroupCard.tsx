import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError, api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { label, tr } from '../lib/i18n'
import { date, isoDay, money, phone, weekdayShort, weekdaysText } from '../lib/format'
import type { Classroom, CField, Group } from '../lib/types'
import { Badge, Banner, Button, Card, Field, Input, Modal, Select, Spinner, StatusBadge, Table, cx, studentStatus } from '../components/ui'
import { cfDisplay } from '../components/CustomFields'
import { Fill, GroupModal } from './Groups'

type Data = {
  group: Group
  fields: CField[]
  students: { id: string; full_name: string; phone: string | null; status: string; joined_at: string; balance_tiyn?: number; attendance_rate: number | null }[]
  left: { id: string; full_name: string; joined_at: string; left_at: string; reason: string | null }[]
  waitlist: { id: string; full_name: string; kind: string; phone: string | null; priority: number }[]
}

export default function GroupCard() {
  const { id } = useParams()
  const { can } = useAuth()
  const qc = useQueryClient()
  const [modal, setModal] = useState<null | 'edit' | 'series' | 'add' | 'wait'>(null)
  const q = useQuery({ queryKey: ['group', id], queryFn: () => api.get<Data>('/groups/' + id) })
  const refresh = () => { qc.invalidateQueries({ queryKey: ['group', id] }); qc.invalidateQueries({ queryKey: ['groups'] }) }
  const leave = useMutation({ mutationFn: ({ sid, reason }: { sid: string; reason: string }) => api.post(`/students/${sid}/groups/${id}/leave`, { reason }), onSuccess: refresh })
  const unwait = useMutation({ mutationFn: (wid: string) => api.del(`/groups/${id}/waitlist/${wid}`), onSuccess: refresh })
  if (q.isLoading || !q.data) return <Spinner />
  const { group: g, students } = q.data
  const full = !!g.capacity && g.students >= g.capacity
  return (
    <div className="mx-auto max-w-[1300px] p-4 lg:p-8">
      <div className="text-[13px] text-muted"><Link to="/groups" className="text-brand-600 hover:underline">{tr('Группы')}</Link> / {g.name}</div>
      <div className="mt-2 flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-[26px] font-bold">{g.name}</h1>
          <div className="mt-1 text-sm text-muted">{g.course_name} · {g.teacher_name ?? tr('без преподавателя')} · {g.classroom_name ?? tr('без кабинета')} · {g.branch_name}{g.type === 'individual' && tr(' · индивидуально')}</div>
        </div>
        {can('groups.edit') && <div className="flex gap-2"><Button onClick={() => setModal('edit')}>{tr('Изменить')}</Button>{can('schedule.edit') && <Button onClick={() => setModal('series')}>{tr('+ Расписание')}</Button>}{g.type === 'group' && <Button variant="primary" onClick={() => setModal(full ? 'wait' : 'add')}>{full ? tr('В лист ожидания') : tr('+ Ученик')}</Button>}</div>}
      </div>

      <div className="mt-6 grid gap-6 xl:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
        <Card title={<span>{tr('Состав ·')} <Fill n={g.students} cap={g.capacity} /></span>} bodyClass="!px-0 !pb-1">
          <Table head={[tr('Ученик'), tr('Статус'), tr('В группе с'), tr('Посещаемость'), ...(can('finance.view') ? [tr('Баланс')] : []), '']}>
            {students.map((s) => (
              <tr key={s.id} className="hover:bg-gray-50">
                <td className="px-4 py-3"><Link to={`/students/${s.id}`} className="font-medium hover:text-brand-600">{s.full_name}</Link><div className="text-xs text-muted">{phone(s.phone)}</div></td>
                <td className="px-4 py-3"><StatusBadge map={studentStatus} value={s.status} /></td>
                <td className="px-4 py-3 text-[13px] text-muted">{date(s.joined_at)}</td>
                <td className="px-4 py-3 tabular">{s.attendance_rate != null ? `${s.attendance_rate}%` : '—'}</td>
                {can('finance.view') && <td className={cx('px-4 py-3 font-semibold tabular', (s.balance_tiyn ?? 0) < 0 && 'text-red-700')}>{money(s.balance_tiyn)}</td>}
                <td className="px-4 py-3 text-right">{can('students.edit') && <Button size="sm" variant="ghost" onClick={() => { const r = prompt(tr('Причина выбытия (переезд, цена, расписание…)')); if (r !== null) leave.mutate({ sid: s.id, reason: r }) }}>{tr('Исключить')}</Button>}</td>
              </tr>
            ))}
          </Table>
          {students.length === 0 && <div className="py-8 text-center text-sm text-muted">{tr('В группе пока нет учеников')}</div>}
        </Card>
        <div className="space-y-5">
          <Card title={tr('Расписание')}>
            {g.schedule.length === 0 ? <div className="text-sm text-muted">{tr('Серия занятий не задана. Нажмите «+ Расписание».')}</div> : g.schedule.map((s) => (
              <div key={s.id} className="py-1.5 text-sm"><b>{weekdaysText(s.weekdays)}</b> · {s.start_time}–{s.end_time}<div className="text-xs text-muted">{tr('с')} {date(s.valid_from)}{s.valid_to ? tr(' по {0}', date(s.valid_to)) : tr(', бессрочно')}</div></div>
            ))}
            <Link to="/schedule" className="mt-2 inline-block text-sm text-brand-600 hover:underline">{tr('Открыть в расписании')}</Link>
          </Card>
          {q.data.fields.length > 0 && (
            <Card title={<span className="flex items-center gap-2">{tr('Параметры')} <span className="rounded bg-brand-50 px-1.5 py-0.5 text-[11px] font-medium text-brand-700">{tr('доп. поля')}</span></span>}>
              <dl className="space-y-3">{q.data.fields.map((f) => <div key={f.key}><dt className="text-[13px] text-muted">{label(f.label)}</dt><dd className="text-sm">{cfDisplay(f, g.custom_fields[f.key])}</dd></div>)}</dl>
            </Card>
          )}
          {g.type === 'group' && (
            <Card title={tr('Лист ожидания · {0}', q.data.waitlist.length)} action={can('groups.edit') && <Button size="sm" variant="ghost" onClick={() => setModal('wait')}>{tr('+ Добавить')}</Button>}>
              {q.data.waitlist.length === 0 ? <div className="text-sm text-muted">{tr('Пусто')}</div> : q.data.waitlist.map((w) => (
                <div key={w.id} className="flex items-center justify-between py-1.5 text-sm"><span>{w.full_name} <Badge>{w.kind === 'lead' ? tr('лид') : tr('ученик')}</Badge></span><button className="text-xs text-muted hover:text-red-700" onClick={() => unwait.mutate(w.id)}>{tr('убрать')}</button></div>
              ))}
            </Card>
          )}
          {q.data.left.length > 0 && (
            <Card title={tr('Выбывшие')}>
              {q.data.left.map((l) => <div key={l.id + l.left_at} className="py-1.5 text-sm"><Link to={`/students/${l.id}`} className="hover:text-brand-600">{l.full_name}</Link><div className="text-xs text-muted">{date(l.joined_at)} – {date(l.left_at)}{l.reason && ` · ${l.reason}`}</div></div>)}
            </Card>
          )}
        </div>
      </div>
      {modal === 'edit' && <GroupModal group={g} individual={g.type === 'individual'} onClose={() => setModal(null)} onSaved={() => { setModal(null); refresh() }} />}
      {modal === 'series' && <SeriesModal group={g} onClose={() => setModal(null)} onSaved={() => { setModal(null); refresh(); qc.invalidateQueries({ queryKey: ['lessons'] }) }} />}
      {modal === 'add' && <AddStudentModal groupId={g.id} onClose={() => setModal(null)} onSaved={() => { setModal(null); refresh() }} />}
      {modal === 'wait' && <WaitlistModal groupId={g.id} onClose={() => setModal(null)} onSaved={() => { setModal(null); refresh() }} />}
    </div>
  )
}

function SeriesModal({ group, onClose, onSaved }: { group: Group; onClose: () => void; onSaved: () => void }) {
  const rooms = useQuery({ queryKey: ['classrooms', group.branch_id], queryFn: () => api.get<Classroom[]>(`/classrooms?branch_id=${group.branch_id}`) })
  const [f, setF] = useState({ weekdays: [] as number[], start_time: '11:00', end_time: '12:30', valid_from: isoDay(), valid_to: group.end_date?.slice(0, 10) ?? '', classroom_id: '' })
  const [conflicts, setConflicts] = useState<{ message: string; kind: string }[]>([])
  const [error, setError] = useState('')
  const m = useMutation({
    mutationFn: (force: boolean) => api.post<{ lessons_created: number }>('/schedules', { group_id: group.id, ...f, classroom_id: f.classroom_id || undefined, force }),
    onSuccess: onSaved,
    onError: (e) => { if (e instanceof ApiError && e.code === 'SCHEDULE_CONFLICT') setConflicts((e.meta?.conflicts as never) ?? []); else setError((e as Error).message) },
  })
  return (
    <Modal open title={tr('Расписание группы (серия)')} onClose={onClose}
      footer={<><Button onClick={onClose}>{tr('Отмена')}</Button>{conflicts.length > 0 && <Button variant="danger" onClick={() => m.mutate(true)}>{tr('Создать с конфликтами')}</Button>}<Button variant="primary" disabled={!f.weekdays.length} loading={m.isPending} onClick={() => m.mutate(false)}>{tr('Создать')}</Button></>}>
      <div className="space-y-4">
        <Field label={tr('Дни недели')}>
          <div className="flex gap-1.5">{weekdayShort.map((d, i) => (
            <button key={d} type="button" onClick={() => setF({ ...f, weekdays: f.weekdays.includes(i + 1) ? f.weekdays.filter((x) => x !== i + 1) : [...f.weekdays, i + 1].sort() })}
              className={cx('h-10 w-11 rounded-lg border text-sm font-medium', f.weekdays.includes(i + 1) ? 'border-brand-500 bg-brand-50 text-brand-700' : 'border-line')}>{d}</button>
          ))}</div>
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label={tr('Начало')}><Input type="time" value={f.start_time} onChange={(e) => setF({ ...f, start_time: e.target.value })} /></Field>
          <Field label={tr('Конец')}><Input type="time" value={f.end_time} onChange={(e) => setF({ ...f, end_time: e.target.value })} /></Field>
          <Field label={tr('С даты')}><Input type="date" value={f.valid_from} onChange={(e) => setF({ ...f, valid_from: e.target.value })} /></Field>
          <Field label={tr('По дату (необязательно)')}><Input type="date" value={f.valid_to} onChange={(e) => setF({ ...f, valid_to: e.target.value })} /></Field>
        </div>
        <Field label={tr('Кабинет')} hint={tr('По умолчанию — кабинет группы')}><Select value={f.classroom_id} onChange={(e) => setF({ ...f, classroom_id: e.target.value })}><option value="">{group.classroom_name ?? '—'}</option>{rooms.data?.map((r) => <option key={r.id} value={r.id}>{r.name}</option>)}</Select></Field>
        <p className="text-xs text-muted">{tr('Занятия создаются на 8 недель вперёд и продлеваются автоматически. Праздники РК и нерабочие дни центра попадают в статус «Требует решения».')}</p>
        {conflicts.slice(0, 5).map((c, i) => <Banner key={i} tone="red">{c.message}</Banner>)}
        {error && <div className="text-sm text-red-700">{error}</div>}
      </div>
    </Modal>
  )
}

function AddStudentModal({ groupId, onClose, onSaved }: { groupId: string; onClose: () => void; onSaved: () => void }) {
  const [q, setQ] = useState('')
  const [error, setError] = useState('')
  const res = useQuery({ queryKey: ['student-pick', q], queryFn: () => api.get<{ id: string; full_name: string; groups: { name: string }[] }[]>(`/students?limit=10&q=${encodeURIComponent(q)}`), enabled: q.length >= 2 })
  const m = useMutation({ mutationFn: (sid: string) => api.post(`/students/${sid}/groups`, { group_id: groupId }), onSuccess: onSaved, onError: (e) => setError((e as Error).message) })
  return (
    <Modal open title={tr('Добавить ученика в группу')} onClose={onClose}>
      <Input autoFocus placeholder={tr('ФИО или телефон')} value={q} onChange={(e) => setQ(e.target.value)} />
      <div className="mt-2 divide-y divide-line">
        {res.data?.map((s) => (
          <button key={s.id} onClick={() => m.mutate(s.id)} className="flex w-full justify-between px-2 py-2.5 text-left text-sm hover:bg-gray-50">
            <span>{s.full_name}</span><span className="text-xs text-muted">{s.groups.map((g) => g.name).join(', ')}</span>
          </button>
        ))}
      </div>
      {error && <div className="mt-2 text-sm text-red-700">{error}</div>}
    </Modal>
  )
}

function WaitlistModal({ groupId, onClose, onSaved }: { groupId: string; onClose: () => void; onSaved: () => void }) {
  const leads = useQuery({ queryKey: ['leads', 'open'], queryFn: () => api.get<{ id: string; full_name: string }[]>('/leads?stage=new,contacted,trial_scheduled,trial_done') })
  const [lead, setLead] = useState('')
  const m = useMutation({ mutationFn: () => api.post(`/groups/${groupId}/waitlist`, { lead_id: lead }), onSuccess: onSaved })
  return (
    <Modal open title={tr('Лист ожидания')} onClose={onClose} footer={<><Button onClick={onClose}>{tr('Отмена')}</Button><Button variant="primary" disabled={!lead} onClick={() => m.mutate()}>{tr('Добавить')}</Button></>}>
      <Field label={tr('Лид')}><Select value={lead} onChange={(e) => setLead(e.target.value)}><option value="">{tr('Выберите')}</option>{leads.data?.map((l) => <option key={l.id} value={l.id}>{l.full_name}</option>)}</Select></Field>
      <p className="mt-2 text-xs text-muted">{tr('Когда в группе освободится место, ответственный получит уведомление.')}</p>
    </Modal>
  )
}
