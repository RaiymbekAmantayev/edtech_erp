import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../../lib/api'
import { useAuth } from '../../lib/auth'
import { relative } from '../../lib/format'
import { Badge, Button, Card, Checkbox, Field, Input, Modal, Select, Spinner, Table, Tabs } from '../../components/ui'

type Role = { id: string; code: string; name: string; is_system: boolean; all_branches: boolean; permissions: string[]; users: number }
type User = { id: string; full_name: string; email: string; status: string; last_login_at: string | null; roles: string[]; branch_ids: string[] }

const permGroups: [string, [string, string][]][] = [
  ['Ученики', [['students.view', 'Просмотр'], ['students.edit', 'Изменение'], ['students.export', 'Экспорт'], ['students.import', 'Импорт'], ['students.view_iin', 'Видеть ИИН']]],
  ['Родители', [['parents.view', 'Просмотр'], ['parents.edit', 'Изменение']]],
  ['Лиды', [['leads.view', 'Просмотр'], ['leads.edit', 'Изменение']]],
  ['Группы и курсы', [['groups.view', 'Просмотр'], ['groups.edit', 'Изменение']]],
  ['Расписание', [['schedule.view', 'Просмотр всего'], ['schedule.edit', 'Изменение']]],
  ['Посещаемость', [['attendance.view', 'Просмотр'], ['attendance.mark', 'Отметка'], ['attendance.edit_closed', 'Изменение закрытой']]],
  ['Тестирование', [['tests.conduct', 'Проведение тестов и ввод результатов']]],
  ['Финансы', [['finance.view', 'Просмотр'], ['payments.create', 'Приём оплат'], ['payments.refund', 'Возвраты и корректировки'], ['subscriptions.edit', 'Абонементы']]],
  ['Прочее', [['reports.view', 'Отчёты'], ['employees.view', 'Сотрудники: просмотр'], ['employees.edit', 'Сотрудники: изменение'], ['audit.view', 'Журнал действий']]],
  ['Администрирование', [['settings.manage', 'Настройки'], ['custom_fields.manage', 'Пользовательские поля'], ['roles.manage', 'Роли и права'], ['branches.manage', 'Филиалы'], ['billing.manage', 'Тариф и подписка']]],
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
      <h1 className="text-[26px] font-bold">Пользователи и роли</h1>
      <Tabs className="mt-5" value={tab} onChange={setTab} items={[{ value: 'users', label: 'Пользователи' }, { value: 'roles', label: 'Роли и права' }, { value: 'audit', label: 'Журнал действий' }]} />
      {tab === 'users' && (
        <Card className="mt-4" bodyClass="!p-0">
          {users.isLoading ? <Spinner /> : (
            <Table head={['Пользователь', 'Роли', 'Филиалы', 'Последний вход', 'Статус']}>
              {users.data?.map((u) => (
                <tr key={u.id} className="cursor-pointer hover:bg-gray-50" onClick={() => setEditUser(u)}>
                  <td className="px-4 py-3"><div className="font-medium">{u.full_name}</div><div className="text-xs text-muted">{u.email}</div></td>
                  <td className="px-4 py-3">{u.roles.map((r) => <Badge key={r} tone="blue" className="mr-1">{roleName(r)}</Badge>)}</td>
                  <td className="px-4 py-3 text-[13px]">{u.branch_ids.length ? u.branch_ids.map((b) => me?.branches?.find((x) => x.id === b)?.name ?? '…').join(', ') : 'все'}</td>
                  <td className="px-4 py-3 text-[13px] text-muted">{relative(u.last_login_at)}</td>
                  <td className="px-4 py-3">{u.status === 'active' ? <Badge tone="green">Активен</Badge> : <Badge tone="red">Заблокирован</Badge>}</td>
                </tr>
              ))}
            </Table>
          )}
          <div className="border-t border-line px-4 py-3 text-xs text-muted">Новые пользователи создаются в разделе «Преподаватели и сотрудники» с флагом «Создать доступ в систему».</div>
        </Card>
      )}
      {tab === 'roles' && (
        <>
          <div className="mt-4 flex justify-end"><Button variant="primary" onClick={() => setEditRole('new')}>+ Роль</Button></div>
          <Card className="mt-3" bodyClass="!p-0">
            <Table head={['Роль', 'Тип', 'Филиалы', 'Прав', 'Пользователей']}>
              {roles.data?.map((r) => (
                <tr key={r.id} className="cursor-pointer hover:bg-gray-50" onClick={() => setEditRole(r)}>
                  <td className="px-4 py-3 font-medium">{r.name}</td>
                  <td className="px-4 py-3 text-[13px]">{r.is_system ? 'системная' : 'своя'}</td>
                  <td className="px-4 py-3 text-[13px]">{r.all_branches ? 'все' : 'назначенные'}</td>
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
          <Table head={['Когда', 'Кто', 'Объект', 'Действие', 'IP']}>
            {audit.data?.map((a) => (
              <tr key={a.id}>
                <td className="whitespace-nowrap px-4 py-2.5 text-[13px] text-muted">{new Date(a.created_at).toLocaleString('ru-RU')}</td>
                <td className="px-4 py-2.5 text-[13px]">{a.user}{a.impersonated && <Badge tone="yellow" className="ml-1">поддержка</Badge>}</td>
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
    <Modal open wide title={role ? role.name : 'Новая роль'} onClose={onClose} footer={!locked && <><Button onClick={onClose}>Отмена</Button><Button variant="primary" loading={m.isPending} onClick={() => m.mutate()}>Сохранить</Button></>}>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Название"><Input value={name} disabled={locked} onChange={(e) => setName(e.target.value)} placeholder="Администратор ресепшн" /></Field>
        {!role && <Field label="На основе роли"><Select value={base} onChange={(e) => { setBase(e.target.value); setPerms(roles.find((r) => r.id === e.target.value)?.permissions ?? []) }}><option value="">— с нуля —</option>{roles.map((r) => <option key={r.id} value={r.id}>{r.name}</option>)}</Select></Field>}
      </div>
      <div className="mt-3"><Checkbox checked={all} disabled={locked} onChange={setAll} label="Доступ ко всем филиалам (иначе — только назначенные)" /></div>
      <div className="mt-5 grid gap-4 sm:grid-cols-2">
        {permGroups.map(([g, ps]) => (
          <div key={g}>
            <div className="mb-1.5 text-sm font-semibold">{g}</div>
            <div className="space-y-1.5">{ps.map(([p, l]) => <Checkbox key={p} disabled={locked} checked={perms.includes(p)} onChange={(v) => { setBase(''); setPerms(v ? [...perms, p] : perms.filter((x) => x !== p)) }} label={<span>{l} <code className="text-[11px] text-muted">{p}</code></span>} />)}</div>
          </div>
        ))}
      </div>
      {locked && <p className="mt-4 text-xs text-muted">Права владельца не редактируются. Системные роли нельзя удалить, но можно скопировать.</p>}
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
    <Modal open title={user.full_name} onClose={onClose} footer={<><Button onClick={onClose}>Отмена</Button><Button variant="primary" loading={m.isPending} onClick={() => m.mutate()}>Сохранить</Button></>}>
      <div className="space-y-4">
        <div><div className="mb-1.5 text-sm font-semibold">Роли (права объединяются)</div><div className="space-y-1.5">{roles.map((x) => <Checkbox key={x.id} checked={r.includes(x.code)} onChange={(v) => setR(v ? [...r, x.code] : r.filter((c) => c !== x.code))} label={x.name} />)}</div></div>
        <div><div className="mb-1.5 text-sm font-semibold">Филиалы</div><div className="space-y-1.5">{me?.branches?.map((x) => <Checkbox key={x.id} checked={b.includes(x.id)} onChange={(v) => setB(v ? [...b, x.id] : b.filter((c) => c !== x.id))} label={x.name} />)}</div></div>
        <Field label="Статус"><Select value={status} onChange={(e) => setStatus(e.target.value)}><option value="active">Активен</option><option value="blocked">Заблокирован (все сессии будут завершены)</option></Select></Field>
        {err && <div className="text-sm text-red-700">{err}</div>}
      </div>
    </Modal>
  )
}
