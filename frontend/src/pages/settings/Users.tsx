import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../../lib/api'
import { useAuth } from '../../lib/auth'
import { relative } from '../../lib/format'
import { Badge, Button, Card, Checkbox, Field, Input, Modal, Select, Spinner, Table, Tabs } from '../../components/ui'
import { tr, intlLocale } from '../../lib/i18n'

type Role = { id: string; code: string; name: string; is_system: boolean; all_branches: boolean; permissions: string[]; users: number }
type User = { id: string; full_name: string; email: string; status: string; last_login_at: string | null; roles: string[]; branch_ids: string[] }

const permGroups: [string, [string, string][]][] = [
  [tr('Ученики'), [['students.view', tr('Просмотр')], ['students.edit', tr('Изменение')], ['students.export', tr('Экспорт')], ['students.import', tr('Импорт')], ['students.view_iin', tr('Видеть ИИН')]]],
  [tr('Родители'), [['parents.view', tr('Просмотр')], ['parents.edit', tr('Изменение')]]],
  [tr('Лиды'), [['leads.view', tr('Просмотр')], ['leads.edit', tr('Изменение')]]],
  [tr('Группы и курсы'), [['groups.view', tr('Просмотр')], ['groups.edit', tr('Изменение')]]],
  [tr('Расписание'), [['schedule.view', tr('Просмотр всего')], ['schedule.edit', tr('Изменение')]]],
  [tr('Посещаемость'), [['attendance.view', tr('Просмотр')], ['attendance.mark', tr('Отметка')], ['attendance.edit_closed', tr('Изменение закрытой')]]],
  [tr('Тестирование'), [['tests.conduct', tr('Проведение тестов и ввод результатов')]]],
  [tr('Финансы'), [['finance.view', tr('Просмотр')], ['payments.create', tr('Приём оплат')], ['payments.refund', tr('Возвраты и корректировки')], ['subscriptions.edit', tr('Абонементы')], ['payroll.view', tr('Зарплата преподавателей')]]],
  [tr('Прочее'), [['reports.view', tr('Отчёты')], ['employees.view', tr('Сотрудники: просмотр')], ['employees.edit', tr('Сотрудники: изменение')], ['audit.view', tr('Журнал действий')]]],
  [tr('Администрирование'), [['settings.manage', tr('Настройки')], ['custom_fields.manage', tr('Пользовательские поля')], ['roles.manage', tr('Роли и права')], ['branches.manage', tr('Филиалы')], ['billing.manage', tr('Тариф и подписка')]]],
]

export default function Users() {
  const { me } = useAuth()
  const qc = useQueryClient()
  const [tab, setTab] = useState<'users' | 'roles' | 'audit'>('users')
  const [editRole, setEditRole] = useState<Role | 'new' | null>(null)
  const [editUser, setEditUser] = useState<User | null>(null)
  const users = useQuery({ queryKey: ['users'], queryFn: () => api.get<User[]>('/users') })
  const roles = useQuery({ queryKey: ['roles'], queryFn: () => api.get<Role[]>('/roles') })
  const audit = useQuery({ queryKey: ['audit'], queryFn: () => api.get<any[]>('/audit'), enabled: tab === 'audit' })
  const roleName = (c: string) => roles.data?.find((r) => r.code === c)?.name ?? c
  return (
    <div className="max-w-5xl">
      <h1 className="text-[26px] font-bold">{tr('Пользователи и роли')}</h1>
      <Tabs className="mt-5" value={tab} onChange={setTab} items={[{ value: 'users', label: tr('Пользователи') }, { value: 'roles', label: tr('Роли и права') }, { value: 'audit', label: tr('Журнал действий') }]} />
      {tab === 'users' && (
        <Card className="mt-4" bodyClass="!p-0">
          {users.isLoading ? <Spinner /> : (
            <Table head={[tr('Пользователь'), tr('Роли'), tr('Филиалы'), tr('Последний вход'), tr('Статус')]}>
              {users.data?.map((u) => (
                <tr key={u.id} className="cursor-pointer hover:bg-gray-50" onClick={() => setEditUser(u)}>
                  <td className="px-4 py-3"><div className="font-medium">{u.full_name}</div><div className="text-xs text-muted">{u.email}</div></td>
                  <td className="px-4 py-3">{u.roles.map((r) => <Badge key={r} tone="blue" className="mr-1">{roleName(r)}</Badge>)}</td>
                  <td className="px-4 py-3 text-[13px]">{u.branch_ids.length ? u.branch_ids.map((b) => me?.branches?.find((x) => x.id === b)?.name ?? '…').join(', ') : tr('все')}</td>
                  <td className="px-4 py-3 text-[13px] text-muted">{relative(u.last_login_at)}</td>
                  <td className="px-4 py-3">{u.status === 'active' ? <Badge tone="green">{tr('Активен')}</Badge> : <Badge tone="red">{tr('Заблокирован')}</Badge>}</td>
                </tr>
              ))}
            </Table>
          )}
          <div className="border-t border-line px-4 py-3 text-xs text-muted">{tr('Новые пользователи создаются в разделе «Преподаватели и сотрудники» с флагом «Создать доступ в систему».')}</div>
        </Card>
      )}
      {tab === 'roles' && (
        <>
          <div className="mt-4 flex justify-end"><Button variant="primary" onClick={() => setEditRole('new')}>{tr('+ Роль')}</Button></div>
          <Card className="mt-3" bodyClass="!p-0">
            <Table head={[tr('Роль'), tr('Тип'), tr('Филиалы'), tr('Прав'), tr('Пользователей')]}>
              {roles.data?.map((r) => (
                <tr key={r.id} className="cursor-pointer hover:bg-gray-50" onClick={() => setEditRole(r)}>
                  <td className="px-4 py-3 font-medium">{r.name}</td>
                  <td className="px-4 py-3 text-[13px]">{r.is_system ? tr('системная') : tr('своя')}</td>
                  <td className="px-4 py-3 text-[13px]">{r.all_branches ? tr('все') : tr('назначенные')}</td>
                  <td className="px-4 py-3 tabular">{r.permissions.length}</td>
                  <td className="px-4 py-3 tabular">{r.users}</td>
                </tr>
              ))}
            </Table>
          </Card>
        </>
      )}
      {tab === 'audit' && (
        <Card className="mt-4" bodyClass="!p-0">
          <Table head={[tr('Когда'), tr('Кто'), tr('Объект'), tr('Действие'), 'IP']}>
            {audit.data?.map((a) => (
              <tr key={a.id}>
                <td className="whitespace-nowrap px-4 py-2.5 text-[13px] text-muted">{new Date(a.created_at).toLocaleString(intlLocale())}</td>
                <td className="px-4 py-2.5 text-[13px]">{a.user}{a.impersonated && <Badge tone="yellow" className="ml-1">{tr('поддержка')}</Badge>}</td>
                <td className="px-4 py-2.5 text-[13px]">{a.entity_type}</td>
                <td className="px-4 py-2.5 text-[13px]">{a.action}</td>
                <td className="px-4 py-2.5 text-[13px] text-muted">{a.ip ?? ''}</td>
              </tr>
            ))}
          </Table>
        </Card>
      )}
      {editRole && <RoleModal role={editRole === 'new' ? undefined : editRole} roles={roles.data ?? []} onClose={() => { setEditRole(null); qc.invalidateQueries({ queryKey: ['roles'] }) }} />}
      {editUser && <UserModal user={editUser} roles={roles.data ?? []} onClose={() => { setEditUser(null); qc.invalidateQueries({ queryKey: ['users'] }) }} />}
    </div>
  )
}

function RoleModal({ role, roles, onClose }: { role?: Role; roles: Role[]; onClose: () => void }) {
  const [name, setName] = useState(role?.name ?? '')
  const [base, setBase] = useState('')
  const [perms, setPerms] = useState<string[]>(role?.permissions ?? [])
  const [all, setAll] = useState(role?.all_branches ?? false)
  const [err, setErr] = useState('')
  const locked = role?.code === 'owner'
  const m = useMutation({
    mutationFn: () => (role ? api.patch('/roles/' + role.id, { name, permissions: perms, all_branches: all }) : api.post('/roles', { name, base_role_id: base || undefined, permissions: base ? undefined : perms, all_branches: all })),
    onSuccess: onClose,
    onError: (e) => setErr((e as Error).message),
  })
  return (
    <Modal open wide title={role ? role.name : tr('Новая роль')} onClose={onClose} footer={!locked && <><Button onClick={onClose}>{tr('Отмена')}</Button><Button variant="primary" loading={m.isPending} onClick={() => m.mutate()}>{tr('Сохранить')}</Button></>}>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={tr('Название')}><Input value={name} disabled={locked} onChange={(e) => setName(e.target.value)} placeholder={tr('Администратор ресепшн')} /></Field>
        {!role && <Field label={tr('На основе роли')}><Select value={base} onChange={(e) => { setBase(e.target.value); setPerms(roles.find((r) => r.id === e.target.value)?.permissions ?? []) }}><option value="">{tr('— с нуля —')}</option>{roles.map((r) => <option key={r.id} value={r.id}>{r.name}</option>)}</Select></Field>}
      </div>
      <div className="mt-3"><Checkbox checked={all} disabled={locked} onChange={setAll} label={tr('Доступ ко всем филиалам (иначе — только назначенные)')} /></div>
      <div className="mt-5 grid gap-4 sm:grid-cols-2">
        {permGroups.map(([g, ps]) => (
          <div key={g}>
            <div className="mb-1.5 text-sm font-semibold">{g}</div>
            <div className="space-y-1.5">{ps.map(([p, l]) => <Checkbox key={p} disabled={locked} checked={perms.includes(p)} onChange={(v) => { setBase(''); setPerms(v ? [...perms, p] : perms.filter((x) => x !== p)) }} label={<span>{l} <code className="text-[11px] text-muted">{p}</code></span>} />)}</div>
          </div>
        ))}
      </div>
      {locked && <p className="mt-4 text-xs text-muted">{tr('Права владельца не редактируются. Системные роли нельзя удалить, но можно скопировать.')}</p>}
      {err && <div className="mt-3 text-sm text-red-700">{err}</div>}
    </Modal>
  )
}

function UserModal({ user, roles, onClose }: { user: User; roles: Role[]; onClose: () => void }) {
  const { me } = useAuth()
  const [r, setR] = useState<string[]>(user.roles)
  const [b, setB] = useState<string[]>(user.branch_ids)
  const [status, setStatus] = useState(user.status)
  const [err, setErr] = useState('')
  const m = useMutation({ mutationFn: () => api.patch('/users/' + user.id, { roles: r, branch_ids: b, status: status !== user.status ? status : undefined }), onSuccess: onClose, onError: (e) => setErr((e as Error).message) })
  return (
    <Modal open title={user.full_name} onClose={onClose} footer={<><Button onClick={onClose}>{tr('Отмена')}</Button><Button variant="primary" loading={m.isPending} onClick={() => m.mutate()}>{tr('Сохранить')}</Button></>}>
      <div className="space-y-4">
        <div><div className="mb-1.5 text-sm font-semibold">{tr('Роли (права объединяются)')}</div><div className="space-y-1.5">{roles.map((x) => <Checkbox key={x.id} checked={r.includes(x.code)} onChange={(v) => setR(v ? [...r, x.code] : r.filter((c) => c !== x.code))} label={x.name} />)}</div></div>
        <div><div className="mb-1.5 text-sm font-semibold">{tr('Филиалы')}</div><div className="space-y-1.5">{me?.branches?.map((x) => <Checkbox key={x.id} checked={b.includes(x.id)} onChange={(v) => setB(v ? [...b, x.id] : b.filter((c) => c !== x.id))} label={x.name} />)}</div></div>
        <Field label={tr('Статус')}><Select value={status} onChange={(e) => setStatus(e.target.value)}><option value="active">{tr('Активен')}</option><option value="blocked">{tr('Заблокирован (все сессии будут завершены)')}</option></Select></Field>
        {err && <div className="text-sm text-red-700">{err}</div>}
      </div>
    </Modal>
  )
}
