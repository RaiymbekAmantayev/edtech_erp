import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { api, errorsByField } from '../lib/api'
import { useAuth } from '../lib/auth'
import { dateTime, parseMoney, phone } from '../lib/format'
import type { Employee } from '../lib/types'
import { payTypes, payrollVisibility, rateText } from '../lib/payroll'
import { Avatar, Badge, Button, Card, Checkbox, Drawer, Field, Input, Modal, Select, Spinner, Table } from '../components/ui'
import { tr } from '../lib/i18n'

const roleNames: Record<string, string> = { teacher: tr('Преподаватель'), org_admin: tr('Администратор'), branch_manager: tr('Управляющий филиалом'), academic: tr('Учебный отдел'), accountant: tr('Бухгалтер'), hr: 'HR', sales: tr('Менеджер продаж') }

export default function Teachers() {
  const { can } = useAuth()
  const [status, setStatus] = useState('active')
  const [modal, setModal] = useState<null | 'new' | Employee>(null)
  const [open, setOpen] = useState<string | null>(null)
  const list = useQuery({ queryKey: ['employees', status], queryFn: () => api.get<Employee[]>(`/employees?status=${status}`) })
  return (
    <div className="mx-auto max-w-[1300px] p-4 lg:p-8">
      <div className="flex items-center justify-between">
        <h1 className="text-[26px] font-bold">{tr('Преподаватели и сотрудники')}</h1>
        {can('employees.edit') && <Button variant="primary" onClick={() => setModal('new')}><Plus className="size-4" />{tr('Сотрудник')}</Button>}
      </div>
      <Select className="mt-5 !h-9 w-48" value={status} onChange={(e) => setStatus(e.target.value)}><option value="active">{tr('Работают')}</option><option value="archived">{tr('В архиве')}</option><option value="all">{tr('Все')}</option></Select>
      <Card className="mt-3" bodyClass="!p-0">
        {list.isLoading ? <Spinner /> : (
          <Table head={[tr('Сотрудник'), tr('Должность'), tr('Предметы'), tr('Групп'), tr('Занятий на неделе'), tr('Оплата'), tr('Доступ')]}>
            {list.data?.map((e) => (
              <tr key={e.id} className="cursor-pointer hover:bg-gray-50" onClick={() => setOpen(e.id)}>
                <td className="px-4 py-3"><div className="flex items-center gap-3"><Avatar name={e.full_name} size="sm" /><div><div className="font-medium">{e.full_name}</div><div className="text-xs text-muted">{phone(e.phone)}</div></div></div></td>
                <td className="px-4 py-3 text-[13px]">{e.position ?? '—'}</td>
                <td className="px-4 py-3 text-[13px]">{e.subjects.join(', ') || '—'}</td>
                <td className="px-4 py-3 tabular">{e.groups_count}</td>
                <td className="px-4 py-3 tabular">{e.week_lessons}</td>
                <td className="px-4 py-3 text-[13px]">{payTypes[e.pay_type]}{e.pay_rate_tiyn != null && ` · ${rateText(e.pay_type, e.pay_rate_tiyn, e.pay_percent)}`}</td>
                <td className="px-4 py-3">{e.user_id ? e.roles.map((r) => <Badge key={r} tone="blue" className="mr-1">{roleNames[r] ?? r}</Badge>) : <span className="text-[13px] text-muted">{tr('нет входа')}</span>}</td>
              </tr>
            ))}
          </Table>
        )}
      </Card>
      {modal && <EmployeeModal employee={modal === 'new' ? undefined : modal} onClose={() => setModal(null)} />}
      {open && <EmployeeDrawer id={open} onClose={() => setOpen(null)} onEdit={(e) => { setOpen(null); setModal(e) }} />}
    </div>
  )
}

function EmployeeDrawer({ id, onClose, onEdit }: { id: string; onClose: () => void; onEdit: (e: Employee) => void }) {
  const { can } = useAuth()
  const q = useQuery({ queryKey: ['employee', id], queryFn: () => api.get<{ employee: Employee; groups: { id: string; name: string; course: string; students: number }[]; month: { done: number; cancelled: number; unmarked: number } }>('/employees/' + id) })
  if (!q.data) return <Drawer open title={tr('Сотрудник')} onClose={onClose}><Spinner /></Drawer>
  const { employee: e, groups, month } = q.data
  return (
    <Drawer open title={e.full_name} onClose={onClose}>
      <div className="space-y-5 text-sm">
        <div className="grid grid-cols-3 gap-3">
          {[[tr('Проведено в месяце'), month.done], [tr('Отменено'), month.cancelled], [tr('Без отметки'), month.unmarked]].map(([l, v]) => <div key={l} className="rounded-xl border border-line p-3"><div className="text-xs text-muted">{l}</div><div className="mt-1 text-xl font-bold tabular">{v}</div></div>)}
        </div>
        <div><div className="mb-2 font-semibold">{tr('Группы')}</div>{groups.map((g) => <div key={g.id} className="py-1">{g.name} · <span className="text-muted">{g.students} {tr('уч.')}</span></div>)}</div>
        <div>{tr('Условия оплаты:')} {payTypes[e.pay_type]} · {rateText(e.pay_type, e.pay_rate_tiyn ?? 0, e.pay_percent)}</div>
        {e.is_teacher && e.status === 'active' && <TelegramAccess id={e.id} />}
        {can('employees.edit') && <Button onClick={() => onEdit(e)}>{tr('Редактировать')}</Button>}
      </div>
    </Drawer>
  )
}

// Доступ преподавателя к кабинету в Telegram: одноразовая ссылка-приглашение на бота школы (7 дней).
function TelegramAccess({ id }: { id: string }) {
  const { can } = useAuth()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['employee-telegram', id], queryFn: () => api.get<{ linked: boolean; invite_expires_at: string | null }>(`/employees/${id}/telegram`) })
  const [link, setLink] = useState<{ link: string; expires_at: string } | null>(null)
  const [error, setError] = useState('')
  const [copied, setCopied] = useState(false)
  const invite = useMutation({
    mutationFn: () => api.post<{ link: string; expires_at: string }>(`/employees/${id}/telegram-invite`),
    onSuccess: (d) => { setLink(d); setError(''); qc.invalidateQueries({ queryKey: ['employee-telegram', id] }); qc.invalidateQueries({ queryKey: ['employees'] }) },
    onError: (e) => setError((e as Error).message),
  })
  const revoke = useMutation({
    mutationFn: () => api.del(`/employees/${id}/telegram`),
    onSuccess: () => { setLink(null); qc.invalidateQueries({ queryKey: ['employee-telegram', id] }) },
  })
  if (!q.data) return null
  return (
    <div className="space-y-3 rounded-xl border border-line p-4">
      <div className="flex items-center justify-between">
        <div className="font-semibold">{tr('Кабинет в Telegram')}</div>
        {q.data.linked ? <Badge tone="green">{tr('Доступ открыт')}</Badge> : q.data.invite_expires_at ? <Badge tone="yellow">{tr('Приглашение отправлено')}</Badge> : <Badge>{tr('Нет доступа')}</Badge>}
      </div>
      <p className="text-[13px] text-muted">{tr('Преподаватель видит своё расписание, статистику и проведённые уроки и отмечает посещаемость прямо в Telegram.')}</p>
      {link && (
        <div className="space-y-2 rounded-lg bg-gray-50 p-3">
          <div className="break-all font-mono text-xs">{link.link}</div>
          <div className="flex items-center gap-2">
            <Button size="sm" onClick={() => { navigator.clipboard?.writeText(link.link); setCopied(true) }}>{copied ? tr('Скопировано') : tr('Копировать ссылку')}</Button>
            <span className="text-xs text-muted">{tr('Одноразовая, действует до')} {dateTime(link.expires_at)}</span>
          </div>
        </div>
      )}
      {error && <div className="text-sm text-red-700">{error}</div>}
      {can('employees.edit') && (
        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant={q.data.linked ? 'secondary' : 'primary'} loading={invite.isPending} onClick={() => { setCopied(false); invite.mutate() }}>
            {q.data.linked ? tr('Новая ссылка (другой телефон)') : tr('Дать доступ — создать ссылку')}
          </Button>
          {(q.data.linked || q.data.invite_expires_at) && <Button size="sm" variant="danger" loading={revoke.isPending} onClick={() => revoke.mutate()}>{tr('Закрыть доступ')}</Button>}
        </div>
      )}
    </div>
  )
}

function EmployeeModal({ employee, onClose }: { employee?: Employee; onClose: () => void }) {
  const { me } = useAuth()
  const qc = useQueryClient()
  const [f, setF] = useState({
    full_name: employee?.full_name ?? '', phone: employee?.phone ?? '', email: employee?.email ?? '', position: employee?.position ?? tr('Преподаватель'), is_teacher: employee?.is_teacher ?? true,
    subjects: employee?.subjects.join(', ') ?? '', branch_ids: employee?.branch_ids ?? (me?.branches?.slice(0, 1).map((b) => b.id) ?? []), pay_type: employee?.pay_type ?? 'per_lesson',
    pay_rate: employee?.pay_rate_tiyn != null ? String(employee.pay_rate_tiyn / 100) : '', status: employee?.status ?? 'active', create_access: false, password: '', roles: ['teacher'],
    pay_pct: employee?.pay_percent ? String(employee.pay_percent / 100) : '', payroll_visible: employee?.payroll_visible ?? 'school',
  })
  const [errors, setErrors] = useState<Record<string, string>>({})
  const m = useMutation({
    mutationFn: () => {
      const { pay_pct, ...rest } = f
      // процент комбинированной модели хранится как процент × 100 — та же точность, что у тиын
      const body = { ...rest, subjects: f.subjects.split(',').map((s) => s.trim()).filter(Boolean), pay_rate_tiyn: parseMoney(f.pay_rate) ?? 0,
        pay_percent: f.pay_type === 'mixed' ? parseMoney(pay_pct) ?? 0 : 0 }
      return employee ? api.patch('/employees/' + employee.id, body) : api.post('/employees', body)
    },
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['employees'] }); onClose() },
    onError: (e) => setErrors({ ...errorsByField(e), _: (e as Error).message }),
  })
  return (
    <Modal open wide title={employee ? tr('Сотрудник') : tr('Новый сотрудник')} onClose={onClose} footer={<><Button onClick={onClose}>{tr('Отмена')}</Button><Button variant="primary" loading={m.isPending} onClick={() => m.mutate()}>{tr('Сохранить')}</Button></>}>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={tr('ФИО')} error={errors.full_name}><Input value={f.full_name} onChange={(e) => setF({ ...f, full_name: e.target.value })} /></Field>
        <Field label={tr('Должность')}><Input value={f.position} onChange={(e) => setF({ ...f, position: e.target.value })} /></Field>
        <Field label={tr('Телефон')} error={errors.phone}><Input value={f.phone} onChange={(e) => setF({ ...f, phone: e.target.value })} /></Field>
        <Field label="Email" error={errors.email}><Input value={f.email} onChange={(e) => setF({ ...f, email: e.target.value })} /></Field>
        <Field label={tr('Предметы (через запятую)')}><Input value={f.subjects} onChange={(e) => setF({ ...f, subjects: e.target.value })} /></Field>
        <Field label={tr('Филиал')}><Select value={f.branch_ids[0] ?? ''} onChange={(e) => setF({ ...f, branch_ids: e.target.value ? [e.target.value] : [] })}>{me?.branches?.map((b) => <option key={b.id} value={b.id}>{b.name}</option>)}</Select></Field>
        <Field label={tr('Модель оплаты')}><Select value={f.pay_type} onChange={(e) => setF({ ...f, pay_type: e.target.value })}>{Object.entries(payTypes).map(([k, l]) => <option key={k} value={k}>{l}</option>)}</Select></Field>
        <Field label={f.pay_type === 'percent' ? tr('Процент от стоимости урока') : f.pay_type === 'hourly' ? tr('Ставка за час, ₸') : f.pay_type === 'fixed' ? tr('Оклад в месяц, ₸') : tr('Ставка за занятие, ₸')}
          hint={f.pay_type === 'percent' ? tr('Например, 40 — преподаватель получает 40% от списанного с учеников за урок') : tr('Начисление считается за каждое проведённое занятие — раздел «Зарплата»')}>
          <Input value={f.pay_rate} onChange={(e) => setF({ ...f, pay_rate: e.target.value })} />
        </Field>
        {f.pay_type === 'mixed' && (
          <Field label={tr('+ Процент от стоимости урока')} error={errors.pay_percent} hint={tr('Например, 10 — к ставке за занятие добавляется 10% от списанного с учеников за урок')}>
            <Input value={f.pay_pct} onChange={(e) => setF({ ...f, pay_pct: e.target.value })} />
          </Field>
        )}
        <Field label={tr('Начисления в кабинете преподавателя')} hint={tr('Видит ли преподаватель свою зарплату в Telegram-кабинете. По умолчанию — как в «Настройки → Зарплата»')}>
          <Select value={f.payroll_visible} onChange={(e) => setF({ ...f, payroll_visible: e.target.value })}>
            {Object.entries(payrollVisibility).map(([k, l]) => <option key={k} value={k}>{l}</option>)}
          </Select>
        </Field>
        <div className="sm:col-span-2"><Checkbox checked={f.is_teacher} onChange={(v) => setF({ ...f, is_teacher: v })} label={tr('Ведёт занятия (преподаватель)')} /></div>
        {employee && <Field label={tr('Статус')}><Select value={f.status} onChange={(e) => setF({ ...f, status: e.target.value })}><option value="active">{tr('Работает')}</option><option value="archived">{tr('В архиве (доступ будет закрыт)')}</option></Select></Field>}
        {!employee && (
          <div className="space-y-3 rounded-xl bg-gray-50 p-4 sm:col-span-2">
            <Checkbox checked={f.create_access} onChange={(v) => setF({ ...f, create_access: v })} label={tr('Создать доступ в систему (вход по email)')} />
            {f.create_access && (
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label={tr('Временный пароль')} error={errors.password}><Input type="text" value={f.password} onChange={(e) => setF({ ...f, password: e.target.value })} placeholder={tr('минимум 10 символов')} /></Field>
                <Field label={tr('Роль')}><Select value={f.roles[0]} onChange={(e) => setF({ ...f, roles: [e.target.value] })}>{Object.entries(roleNames).map(([k, l]) => <option key={k} value={k}>{l}</option>)}</Select></Field>
              </div>
            )}
          </div>
        )}
      </div>
      {errors._ && !Object.keys(errors).some((k) => k !== '_') && <div className="mt-3 text-sm text-red-700">{errors._}</div>}
    </Modal>
  )
}
