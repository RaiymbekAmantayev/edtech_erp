import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { api, errorsByField } from '../lib/api'
import { useAuth } from '../lib/auth'
import { money, parseMoney, phone } from '../lib/format'
import type { Employee } from '../lib/types'
import { Avatar, Badge, Button, Card, Checkbox, Drawer, Field, Input, Modal, Select, Spinner, Table } from '../components/ui'

const payTypes: Record<string, string> = { fixed: 'Фиксированная', per_lesson: 'За занятие', hourly: 'Почасовая', percent: 'Процент', mixed: 'Комбинированная' }
const roleNames: Record<string, string> = { teacher: 'Преподаватель', org_admin: 'Администратор', branch_manager: 'Управляющий филиалом', academic: 'Учебный отдел', accountant: 'Бухгалтер', hr: 'HR', sales: 'Менеджер продаж' }

export default function Teachers() {
  const { can } = useAuth()
  const [status, setStatus] = useState('active')
  const [modal, setModal] = useState<null | 'new' | Employee>(null)
  const [open, setOpen] = useState<string | null>(null)
  const list = useQuery({ queryKey: ['employees', status], queryFn: () => api.get<Employee[]>(`/employees?status=${status}`) })
  return (
    <div className="mx-auto max-w-[1300px] p-4 lg:p-8">
      <div className="flex items-center justify-between">
        <h1 className="text-[26px] font-bold">Преподаватели и сотрудники</h1>
        {can('employees.edit') && <Button variant="primary" onClick={() => setModal('new')}><Plus className="size-4" />Сотрудник</Button>}
      </div>
      <Select className="mt-5 !h-9 w-48" value={status} onChange={(e) => setStatus(e.target.value)}><option value="active">Работают</option><option value="archived">В архиве</option><option value="all">Все</option></Select>
      <Card className="mt-3" bodyClass="!p-0">
        {list.isLoading ? <Spinner /> : (
          <Table head={['Сотрудник', 'Должность', 'Предметы', 'Групп', 'Занятий на неделе', 'Оплата', 'Доступ']}>
            {list.data?.map((e) => (
              <tr key={e.id} className="cursor-pointer hover:bg-gray-50" onClick={() => setOpen(e.id)}>
                <td className="px-4 py-3"><div className="flex items-center gap-3"><Avatar name={e.full_name} size="sm" /><div><div className="font-medium">{e.full_name}</div><div className="text-xs text-muted">{phone(e.phone)}</div></div></div></td>
                <td className="px-4 py-3 text-[13px]">{e.position ?? '—'}</td>
                <td className="px-4 py-3 text-[13px]">{e.subjects.join(', ') || '—'}</td>
                <td className="px-4 py-3 tabular">{e.groups_count}</td>
                <td className="px-4 py-3 tabular">{e.week_lessons}</td>
                <td className="px-4 py-3 text-[13px]">{payTypes[e.pay_type]}{e.pay_rate_tiyn != null && ` · ${money(e.pay_rate_tiyn)}`}</td>
                <td className="px-4 py-3">{e.user_id ? e.roles.map((r) => <Badge key={r} tone="blue" className="mr-1">{roleNames[r] ?? r}</Badge>) : <span className="text-[13px] text-muted">нет входа</span>}</td>
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
  if (!q.data) return <Drawer open title="Сотрудник" onClose={onClose}><Spinner /></Drawer>
  const { employee: e, groups, month } = q.data
  return (
    <Drawer open title={e.full_name} onClose={onClose}>
      <div className="space-y-5 text-sm">
        <div className="grid grid-cols-3 gap-3">
          {[['Проведено в месяце', month.done], ['Отменено', month.cancelled], ['Без отметки', month.unmarked]].map(([l, v]) => <div key={l} className="rounded-xl border border-line p-3"><div className="text-xs text-muted">{l}</div><div className="mt-1 text-xl font-bold tabular">{v}</div></div>)}
        </div>
        <div><div className="mb-2 font-semibold">Группы</div>{groups.map((g) => <div key={g.id} className="py-1">{g.name} · <span className="text-muted">{g.students} уч.</span></div>)}</div>
        <div>Условия оплаты: {payTypes[e.pay_type]} · {money(e.pay_rate_tiyn)}</div>
        {can('employees.edit') && <Button onClick={() => onEdit(e)}>Редактировать</Button>}
      </div>
    </Drawer>
  )
}

function EmployeeModal({ employee, onClose }: { employee?: Employee; onClose: () => void }) {
  const { me } = useAuth()
  const qc = useQueryClient()
  const [f, setF] = useState({
    full_name: employee?.full_name ?? '', phone: employee?.phone ?? '', email: employee?.email ?? '', position: employee?.position ?? 'Преподаватель', is_teacher: employee?.is_teacher ?? true,
    subjects: employee?.subjects.join(', ') ?? '', branch_ids: employee?.branch_ids ?? (me?.branches?.slice(0, 1).map((b) => b.id) ?? []), pay_type: employee?.pay_type ?? 'per_lesson',
    pay_rate: employee?.pay_rate_tiyn != null ? String(employee.pay_rate_tiyn / 100) : '', status: employee?.status ?? 'active', create_access: false, password: '', roles: ['teacher'],
  })
  const [errors, setErrors] = useState<Record<string, string>>({})
  const m = useMutation({
    mutationFn: () => {
      const body = { ...f, subjects: f.subjects.split(',').map((s) => s.trim()).filter(Boolean), pay_rate_tiyn: parseMoney(f.pay_rate) ?? 0 }
      return employee ? api.patch('/employees/' + employee.id, body) : api.post('/employees', body)
    },
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['employees'] }); onClose() },
    onError: (e) => setErrors({ ...errorsByField(e), _: (e as Error).message }),
  })
  return (
    <Modal open wide title={employee ? 'Сотрудник' : 'Новый сотрудник'} onClose={onClose} footer={<><Button onClick={onClose}>Отмена</Button><Button variant="primary" loading={m.isPending} onClick={() => m.mutate()}>Сохранить</Button></>}>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="ФИО" error={errors.full_name}><Input value={f.full_name} onChange={(e) => setF({ ...f, full_name: e.target.value })} /></Field>
        <Field label="Должность"><Input value={f.position} onChange={(e) => setF({ ...f, position: e.target.value })} /></Field>
        <Field label="Телефон" error={errors.phone}><Input value={f.phone} onChange={(e) => setF({ ...f, phone: e.target.value })} /></Field>
        <Field label="Email" error={errors.email}><Input value={f.email} onChange={(e) => setF({ ...f, email: e.target.value })} /></Field>
        <Field label="Предметы (через запятую)"><Input value={f.subjects} onChange={(e) => setF({ ...f, subjects: e.target.value })} /></Field>
        <Field label="Филиал"><Select value={f.branch_ids[0] ?? ''} onChange={(e) => setF({ ...f, branch_ids: e.target.value ? [e.target.value] : [] })}>{me?.branches?.map((b) => <option key={b.id} value={b.id}>{b.name}</option>)}</Select></Field>
        <Field label="Модель оплаты"><Select value={f.pay_type} onChange={(e) => setF({ ...f, pay_type: e.target.value })}>{Object.entries(payTypes).map(([k, l]) => <option key={k} value={k}>{l}</option>)}</Select></Field>
        <Field label="Ставка, ₸"><Input value={f.pay_rate} onChange={(e) => setF({ ...f, pay_rate: e.target.value })} /></Field>
        <div className="sm:col-span-2"><Checkbox checked={f.is_teacher} onChange={(v) => setF({ ...f, is_teacher: v })} label="Ведёт занятия (преподаватель)" /></div>
        {employee && <Field label="Статус"><Select value={f.status} onChange={(e) => setF({ ...f, status: e.target.value })}><option value="active">Работает</option><option value="archived">В архиве (доступ будет закрыт)</option></Select></Field>}
        {!employee && (
          <div className="space-y-3 rounded-xl bg-gray-50 p-4 sm:col-span-2">
            <Checkbox checked={f.create_access} onChange={(v) => setF({ ...f, create_access: v })} label="Создать доступ в систему (вход по email)" />
            {f.create_access && (
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="Временный пароль" error={errors.password}><Input type="text" value={f.password} onChange={(e) => setF({ ...f, password: e.target.value })} placeholder="минимум 10 символов" /></Field>
                <Field label="Роль"><Select value={f.roles[0]} onChange={(e) => setF({ ...f, roles: [e.target.value] })}>{Object.entries(roleNames).map(([k, l]) => <option key={k} value={k}>{l}</option>)}</Select></Field>
              </div>
            )}
          </div>
        )}
      </div>
      {errors._ && !Object.keys(errors).some((k) => k !== '_') && <div className="mt-3 text-sm text-red-700">{errors._}</div>}
    </Modal>
  )
}
