import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, errorsByField } from '../../lib/api'
import { useAuth } from '../../lib/auth'
import { date } from '../../lib/format'
import type { Classroom } from '../../lib/types'
import { Badge, Button, Card, Checkbox, Field, Input, Modal, Select, Spinner, Table, Toast, attendanceStatus } from '../../components/ui'
import { tr } from '../../lib/i18n'

type Org = { id: string; name: string; center_type: string; city: string | null; bin: string | null; timezone: string; locale: string; plan: string; status: string; settings: Record<string, any>; trial_ends_at: string | null }
type BranchRow = { id: string; name: string; address: string | null; phone: string | null; timezone: string; is_active: boolean; classrooms: number; students: number }

const useOrg = () => useQuery({ queryKey: ['organization'], queryFn: () => api.get<Org>('/organization') })

export function Organization() {
  const qc = useQueryClient()
  const org = useOrg()
  const [f, setF] = useState<Record<string, any> | null>(null)
  const [toast, setToast] = useState('')
  const m = useMutation({
    mutationFn: () => api.patch('/organization', f),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['organization'] }); setToast(tr('Сохранено')) },
    onError: (e) => setToast((e as Error).message),
  })
  if (!org.data) return <Spinner />
  const o = org.data
  const v = f ?? { name: o.name, city: o.city ?? '', bin: o.bin ?? '', timezone: o.timezone, locale: o.locale, settings: { ...o.settings } }
  const set = (k: string, val: unknown) => setF({ ...v, [k]: val })
  const setS = (k: string, val: unknown) => setF({ ...v, settings: { ...v.settings, [k]: val } })
  return (
    <div className="max-w-3xl space-y-5">
      <h1 className="text-[26px] font-bold">{tr('Организация')}</h1>
      <Card title={tr('Основное')}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={tr('Название')}><Input value={v.name} onChange={(e) => set('name', e.target.value)} /></Field>
          <Field label={tr('Город')}><Input value={v.city} onChange={(e) => set('city', e.target.value)} /></Field>
          <Field label={tr('БИН')}><Input value={v.bin} maxLength={12} onChange={(e) => set('bin', e.target.value.replace(/\D/g, ''))} /></Field>
          <Field label={tr('Часовой пояс')} hint={tr('IANA-идентификатор. Казахстан — единый UTC+5 с 01.03.2024')}>
            <Select value={v.timezone} onChange={(e) => set('timezone', e.target.value)}><option>Asia/Almaty</option><option>Asia/Aqtobe</option><option>Asia/Oral</option><option>Asia/Qostanay</option><option>Asia/Aqtau</option><option>Asia/Atyrau</option></Select>
          </Field>
          <Field label={tr('Язык по умолчанию')}><Select value={v.locale} onChange={(e) => set('locale', e.target.value)}><option value="ru">{tr('Русский')}</option><option value="kk">{tr('Қазақша')}</option><option value="en">English</option></Select></Field>
        </div>
      </Card>
      <Card title={tr('Учебный процесс')}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={tr('Занятия в нерабочие дни')}>
            <Select value={v.settings.lesson_on_holiday ?? 'needs_decision'} onChange={(e) => setS('lesson_on_holiday', e.target.value)}>
              <option value="needs_decision">{tr('Создавать со статусом «Требует решения»')}</option><option value="skip">{tr('Не создавать')}</option>
            </Select>
          </Field>
          <Field label={tr('Редактирование посещаемости после закрытия, ч')}><Input type="number" value={v.settings.edit_closed_hours ?? 24} onChange={(e) => setS('edit_closed_hours', Number(e.target.value))} /></Field>
          <Field label={tr('Уважительных пропусков в месяц без списания')} hint={tr('0 — без ограничения')}><Input type="number" value={v.settings.excused_limit ?? 0} onChange={(e) => setS('excused_limit', Number(e.target.value))} /></Field>
          <Field label={tr('Занятий в долг (кредит)')}><Input type="number" value={v.settings.credit_lessons ?? 2} onChange={(e) => setS('credit_lessons', Number(e.target.value))} /></Field>
          <Field label={tr('Максимум дней заморозки на абонемент')}><Input type="number" value={v.settings.freeze_max_days ?? 30} onChange={(e) => setS('freeze_max_days', Number(e.target.value))} /></Field>
          <Field label={tr('Заморозок на абонемент')}><Input type="number" value={v.settings.freeze_max_count ?? 2} onChange={(e) => setS('freeze_max_count', Number(e.target.value))} /></Field>
        </div>
      </Card>
      <Card title={tr('Уведомления родителям')}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={tr('Тихие часы с')}><Input type="time" value={v.settings.quiet_hours?.from ?? '22:00'} onChange={(e) => setS('quiet_hours', { ...v.settings.quiet_hours, from: e.target.value })} /></Field>
          <Field label={tr('до')}><Input type="time" value={v.settings.quiet_hours?.to ?? '08:00'} onChange={(e) => setS('quiet_hours', { ...v.settings.quiet_hours, to: e.target.value })} /></Field>
        </div>
        <p className="mt-3 text-xs text-muted">{tr('Ночью сообщения не отправляются, а откладываются до утра по времени центра.')}</p>
      </Card>
      <Card title={tr('Справочники')}>
        <Field label={tr('Источники лидов (через запятую)')}><Input value={(v.settings.lead_sources ?? []).join(', ')} onChange={(e) => setS('lead_sources', e.target.value.split(',').map((x) => x.trim()).filter(Boolean))} /></Field>
        <Field className="mt-4" label={tr('Причины отказа (через запятую)')}><Input value={(v.settings.lost_reasons ?? []).join(', ')} onChange={(e) => setS('lost_reasons', e.target.value.split(',').map((x) => x.trim()).filter(Boolean))} /></Field>
        <Field className="mt-4" label={tr('Публичная форма заявок с сайта')} hint={tr('POST JSON {full_name, phone, comment} — заявка попадёт в «Лиды» с источником «Сайт»')}>
          <Input readOnly value={`${location.origin}/api/v1/public/${o.id}/leads`} onFocus={(e) => e.target.select()} />
        </Field>
      </Card>
      <Button variant="primary" disabled={!f} loading={m.isPending} onClick={() => m.mutate()}>{tr('Сохранить')}</Button>
      {toast && <Toast text={toast} onDone={() => setToast('')} />}
    </div>
  )
}

export function Branches() {
  const qc = useQueryClient()
  const branches = useQuery({ queryKey: ['branches'], queryFn: () => api.get<BranchRow[]>('/branches') })
  const rooms = useQuery({ queryKey: ['classrooms', 'all'], queryFn: () => api.get<Classroom[]>('/classrooms') })
  const [modal, setModal] = useState<null | { branch?: BranchRow } | { room: Partial<Classroom> }>(null)
  const refresh = () => { qc.invalidateQueries({ queryKey: ['branches'] }); qc.invalidateQueries({ queryKey: ['classrooms'] }); qc.invalidateQueries({ queryKey: ['me'] }) }
  return (
    <div className="max-w-5xl space-y-5">
      <div className="flex items-center justify-between"><h1 className="text-[26px] font-bold">{tr('Филиалы и кабинеты')}</h1><Button variant="primary" onClick={() => setModal({})}>{tr('+ Филиал')}</Button></div>
      {branches.isLoading && <Spinner />}
      {branches.data?.map((b) => (
        <Card key={b.id} title={<span>{b.name}{!b.is_active && <Badge className="ml-2">{tr('неактивен')}</Badge>}</span>}
          action={<div className="flex gap-2"><Button size="sm" onClick={() => setModal({ room: { branch_id: b.id } })}>{tr('+ Кабинет')}</Button><Button size="sm" variant="ghost" onClick={() => setModal({ branch: b })}>{tr('Изменить')}</Button></div>}>
          <div className="mb-3 text-sm text-muted">{b.address ?? tr('адрес не указан')} · {b.timezone} · {b.students} {tr('активных учеников')}</div>
          <div className="flex flex-wrap gap-2">
            {rooms.data?.filter((r) => r.branch_id === b.id).map((r) => (
              <button key={r.id} onClick={() => setModal({ room: r })} className="rounded-lg border border-line px-3 py-2 text-left text-sm hover:bg-gray-50">
                <div className="font-medium">{r.name}{r.status === 'repair' && <Badge tone="yellow" className="ml-1.5">{tr('ремонт')}</Badge>}</div>
                <div className="text-xs text-muted">{r.is_online ? tr('онлайн · без лимита') : tr('{0} мест', r.capacity ?? '—')}{r.equipment ? ` · ${r.equipment}` : ''}</div>
              </button>
            ))}
          </div>
        </Card>
      ))}
      {modal && 'room' in modal ? <RoomModal room={modal.room} onClose={() => { setModal(null); refresh() }} /> : modal && <BranchModal branch={(modal as { branch?: BranchRow }).branch} onClose={() => { setModal(null); refresh() }} />}
    </div>
  )
}

function BranchModal({ branch, onClose }: { branch?: BranchRow; onClose: () => void }) {
  const [f, setF] = useState({ name: branch?.name ?? '', address: branch?.address ?? '', phone: branch?.phone ?? '', timezone: branch?.timezone ?? 'Asia/Almaty', is_active: branch?.is_active ?? true })
  const [err, setErr] = useState('')
  const m = useMutation({ mutationFn: () => (branch ? api.patch('/branches/' + branch.id, f) : api.post('/branches', f)), onSuccess: onClose, onError: (e) => setErr((e as Error).message) })
  return (
    <Modal open title={branch ? tr('Филиал') : tr('Новый филиал')} onClose={onClose} footer={<><Button onClick={onClose}>{tr('Отмена')}</Button><Button variant="primary" loading={m.isPending} onClick={() => m.mutate()}>{tr('Сохранить')}</Button></>}>
      <div className="space-y-4">
        <Field label={tr('Название')}><Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} /></Field>
        <Field label={tr('Адрес')}><Input value={f.address} onChange={(e) => setF({ ...f, address: e.target.value })} /></Field>
        <Field label={tr('Телефон')}><Input value={f.phone} onChange={(e) => setF({ ...f, phone: e.target.value })} /></Field>
        <Field label={tr('Часовой пояс')}><Input value={f.timezone} onChange={(e) => setF({ ...f, timezone: e.target.value })} /></Field>
        {branch && <Checkbox checked={f.is_active} onChange={(v) => setF({ ...f, is_active: v })} label={tr('Филиал работает')} />}
        {err && <div className="text-sm text-red-700">{err}</div>}
      </div>
    </Modal>
  )
}

function RoomModal({ room, onClose }: { room: Partial<Classroom>; onClose: () => void }) {
  const [f, setF] = useState({ name: room.name ?? '', capacity: room.capacity ? String(room.capacity) : '', is_online: room.is_online ?? false, equipment: room.equipment ?? '', status: room.status ?? 'active' })
  const m = useMutation({
    mutationFn: () => {
      const body = { ...f, branch_id: room.branch_id, capacity: f.capacity ? Number(f.capacity) : null }
      return room.id ? api.patch('/classrooms/' + room.id, body) : api.post('/classrooms', body)
    },
    onSuccess: onClose,
  })
  return (
    <Modal open title={room.id ? room.name! : tr('Новый кабинет')} onClose={onClose} footer={<><Button onClick={onClose}>{tr('Отмена')}</Button><Button variant="primary" loading={m.isPending} onClick={() => m.mutate()}>{tr('Сохранить')}</Button></>}>
      <div className="space-y-4">
        <Field label={tr('Название / номер')}><Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} /></Field>
        {!room.id && <Checkbox checked={f.is_online} onChange={(v) => setF({ ...f, is_online: v })} label={tr('Онлайн (Zoom/Meet) — без проверки вместимости')} />}
        {!f.is_online && <Field label={tr('Вместимость')}><Input type="number" value={f.capacity} onChange={(e) => setF({ ...f, capacity: e.target.value })} /></Field>}
        <Field label={tr('Оснащение')}><Input value={f.equipment} onChange={(e) => setF({ ...f, equipment: e.target.value })} placeholder={tr('Проектор, компьютеры')} /></Field>
        {room.id && <Field label={tr('Статус')}><Select value={f.status} onChange={(e) => setF({ ...f, status: e.target.value })}><option value="active">{tr('Активен')}</option><option value="repair">{tr('На ремонте')}</option></Select></Field>}
      </div>
    </Modal>
  )
}

export function Rules() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['att-rules'], queryFn: () => api.get<{ status: string; charge: boolean; pay_teacher: boolean }[]>('/attendance-rules') })
  const [rules, setRules] = useState<{ status: string; charge: boolean; pay_teacher: boolean }[] | null>(null)
  const [toast, setToast] = useState('')
  const m = useMutation({ mutationFn: () => api.put('/attendance-rules', rules), onSuccess: () => { qc.invalidateQueries({ queryKey: ['att-rules'] }); setToast(tr('Сохранено')) } })
  const list = rules ?? q.data ?? []
  const set = (i: number, k: 'charge' | 'pay_teacher', v: boolean) => setRules(list.map((r, j) => (j === i ? { ...r, [k]: v } : r)))
  return (
    <div className="max-w-3xl space-y-5">
      <h1 className="text-[26px] font-bold">{tr('Правила списания')}</h1>
      <p className="text-sm text-muted">{tr('Какой статус посещаемости списывает занятие с абонемента и учитывается в зарплате преподавателя. Изменение отметки задним числом пересчитывается корректирующей операцией, а не перезаписью.')}</p>
      <Card bodyClass="!p-0">
        <Table head={[tr('Статус'), tr('Списывать с абонемента'), tr('Учитывать в зарплате')]}>
          {list.map((r, i) => (
            <tr key={r.status}>
              <td className="px-4 py-3 font-medium">{r.status === 'excused' ? tr('Уважительная причина') : attendanceStatus[r.status]?.[0] ?? r.status}</td>
              <td className="px-4 py-3"><Checkbox checked={r.charge} disabled={r.status === 'trial'} onChange={(v) => set(i, 'charge', v)} /></td>
              <td className="px-4 py-3"><Checkbox checked={r.pay_teacher} onChange={(v) => set(i, 'pay_teacher', v)} /></td>
            </tr>
          ))}
        </Table>
      </Card>
      <Button variant="primary" disabled={!rules} loading={m.isPending} onClick={() => m.mutate()}>{tr('Сохранить')}</Button>
      {toast && <Toast text={toast} onDone={() => setToast('')} />}
    </div>
  )
}

export function Calendar() {
  const qc = useQueryClient()
  const [year, setYear] = useState(new Date().getFullYear())
  const q = useQuery({ queryKey: ['holidays', year], queryFn: () => api.get<{ id: string; day: string; name: string; is_workday: boolean; global: boolean }[]>(`/holidays?year=${year}`) })
  const [f, setF] = useState({ day: '', day_to: '', name: '' })
  const [errors, setErrors] = useState<Record<string, string>>({})
  const add = useMutation({ mutationFn: () => api.post('/holidays', f), onSuccess: () => { setF({ day: '', day_to: '', name: '' }); qc.invalidateQueries({ queryKey: ['holidays'] }); qc.invalidateQueries({ queryKey: ['lessons'] }) }, onError: (e) => setErrors({ _: (e as Error).message, ...errorsByField(e) }) })
  const del = useMutation({ mutationFn: (id: string) => api.del('/holidays/' + id), onSuccess: () => qc.invalidateQueries({ queryKey: ['holidays'] }) })
  return (
    <div className="max-w-3xl space-y-5">
      <div className="flex items-center justify-between">
        <h1 className="text-[26px] font-bold">{tr('Календарь и праздники')}</h1>
        <Select className="!h-9 w-28" value={year} onChange={(e) => setYear(Number(e.target.value))}>{[year - 1, year, year + 1].map((y) => <option key={y}>{y}</option>)}</Select>
      </div>
      <Card title={tr('Нерабочие дни центра')} >
        <div className="grid gap-3 sm:grid-cols-[1fr_1fr_2fr_auto] sm:items-end">
          <Field label={tr('С')}><Input type="date" value={f.day} onChange={(e) => setF({ ...f, day: e.target.value })} /></Field>
          <Field label={tr('По (необязательно)')}><Input type="date" value={f.day_to} onChange={(e) => setF({ ...f, day_to: e.target.value })} /></Field>
          <Field label={tr('Название')}><Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} placeholder={tr('Каникулы')} /></Field>
          <Button variant="primary" loading={add.isPending} onClick={() => add.mutate()}>{tr('Добавить')}</Button>
        </div>
        {errors._ && <div className="mt-2 text-sm text-red-700">{errors._}</div>}
        <p className="mt-2 text-xs text-muted">{tr('Уже созданные занятия в эти дни получат статус «Требует решения».')}</p>
      </Card>
      <Card bodyClass="!p-0">
        {q.isLoading ? <Spinner /> : (
          <Table head={[tr('Дата'), tr('Название'), tr('Источник'), '']}>
            {q.data?.map((h) => (
              <tr key={h.id}>
                <td className="px-4 py-2.5 tabular">{date(h.day + 'T12:00:00+05:00')}</td>
                <td className="px-4 py-2.5">{h.name}{h.is_workday && <Badge className="ml-2">{tr('рабочий день')}</Badge>}</td>
                <td className="px-4 py-2.5 text-[13px] text-muted">{h.global ? tr('Производственный календарь РК') : tr('Центр')}</td>
                <td className="px-4 py-2.5 text-right">{!h.global && <button className="text-xs text-red-700" onClick={() => del.mutate(h.id)}>{tr('удалить')}</button>}</td>
              </tr>
            ))}
          </Table>
        )}
      </Card>
    </div>
  )
}

export function Plan() {
  const { me } = useAuth()
  const org = useOrg()
  const statusText: Record<string, string> = { trial: tr('Пробный период'), active: tr('Активна'), past_due: tr('Просрочка оплаты'), suspended: tr('Приостановлена') }
  if (!org.data) return <Spinner />
  return (
    <div className="max-w-3xl space-y-5">
      <h1 className="text-[26px] font-bold">{tr('Тариф и оплата')}</h1>
      <Card>
        <dl className="grid gap-4 sm:grid-cols-3 text-sm">
          <div><dt className="text-muted">{tr('Тариф')}</dt><dd className="text-lg font-semibold capitalize">{org.data.plan}</dd></div>
          <div><dt className="text-muted">{tr('Статус')}</dt><dd className="text-lg font-semibold">{statusText[org.data.status] ?? org.data.status}</dd></div>
          {org.data.trial_ends_at && <div><dt className="text-muted">{tr('Пробный период до')}</dt><dd className="text-lg font-semibold">{date(org.data.trial_ends_at)}</dd></div>}
        </dl>
      </Card>
      <Card title={tr('Тарифы')}>
        <Table head={[tr('Тариф'), tr('Активных учеников'), tr('Цена в месяц')]}>
          {[['Starter', tr('до 100'), '7 900 ₸'], ['Basic', tr('до 300'), '14 900 ₸'], ['Business', tr('до 700'), '29 900 ₸'], ['Pro', tr('до 1 700'), '49 900 ₸'], ['Enterprise', tr('более 1 700'), tr('по договорённости')]].map(([n, s, p]) => (
            <tr key={n} className={n.toLowerCase() === org.data.plan ? 'bg-brand-50' : ''}><td className="px-4 py-2.5 font-medium">{n}</td><td className="px-4 py-2.5">{s}</td><td className="px-4 py-2.5 tabular">{p}</td></tr>
          ))}
        </Table>
        <p className="mt-3 text-xs text-muted">{tr('Активный ученик — тот, у кого в расчётном месяце было занятие со списанием, активное членство в группе или действующий абонемент. При превышении лимита даётся 14 дней на смену тарифа. Смена тарифа и оплата по счёту (Kaspi) — через поддержку')}{me?.roles?.includes('owner') ? '' : tr(' (доступно владельцу)')}.</p>
      </Card>
    </div>
  )
}
