import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { MoreHorizontal } from 'lucide-react'
import { ApiError, api, errorsByField } from '../lib/api'
import { useAuth } from '../lib/auth'
import { label, tr, intlLocale } from '../lib/i18n'
import { age, date, dateShort, dateTime, money, phone } from '../lib/format'
import type { CField, StudentRow, Subscription } from '../lib/types'
import { Avatar, Badge, Banner, Button, Card, Checkbox, Field, Input, Modal, Select, Spinner, StatusBadge, Tabs, Textarea, Toast, attendanceStatus, cx, paymentMethods, studentStatus } from '../components/ui'
import { CFInput, cfDisplay, groupByBlock, inScope, useBlocks } from '../components/CustomFields'
import { Dropdown, MenuItem } from '../components/Layout'
import { StudentTests } from '../components/Tests'
import { PaymentModal } from './finance/PaymentModal'
import { FreezeModal, SubscriptionModal } from './finance/SubscriptionModals'

type Parent = { id: string; full_name: string; phone: string | null; email: string | null; telegram_linked: boolean; telegram_link_code: string | null; telegram_link: string | null; relation: string; is_payer: boolean; is_legal_rep: boolean; consent_at: string | null }
type Card = {
  student: StudentRow
  details: { gender: string | null; address: string | null; comment: string | null; responsible_id: string | null; responsible_name: string | null; version: number; started_at: string | null; branch_name: string }
  fields: CField[]
  parents: Parent[]
  groups: { id: string; name: string; course: string; type: string; teacher: string | null; classroom: string | null; schedule: string | null }[]
  attendance: { recent: { date: string; status: string; reason: string | null }[]; rate: number | null }
  subscriptions?: (Subscription & { lessons_used: number })[]
  comments: { id: string; body: string; created_at: string; author: string | null }[]
}
type HistoryItem = { id: number; action: string; old: Record<string, unknown> | null; new: Record<string, unknown> | null; created_at: string; user: string }

const relationText: Record<string, string> = { mother: tr('мама'), father: tr('папа'), guardian: tr('опекун'), parent: tr('родитель'), other: tr('контакт') }
const days = ['', tr('Пн'), tr('Вт'), tr('Ср'), tr('Чт'), tr('Пт'), tr('Сб'), tr('Вс')]

export default function StudentCard() {
  const { id } = useParams()
  const { can } = useAuth()
  const qc = useQueryClient()
  const [tab, setTab] = useState<'overview' | 'attendance' | 'tests' | 'finance' | 'documents' | 'history'>('overview')
  const [modal, setModal] = useState<null | 'edit' | { block: string | null } | 'payment' | 'subscription' | 'freeze' | 'parent' | 'adjust' | 'status'>(null)
  const [menu, setMenu] = useState(false)
  const [toast, setToast] = useState('')
  const [iin, setIin] = useState<string | null>(null)
  const card = useQuery({ queryKey: ['student', id], queryFn: () => api.get<Card>('/students/' + id) })
  const history = useQuery({ queryKey: ['student-history', id], queryFn: () => api.get<HistoryItem[]>(`/students/${id}/history?limit=50`) })
  const blocks = useBlocks('student')
  const refresh = () => { qc.invalidateQueries({ queryKey: ['student', id] }); qc.invalidateQueries({ queryKey: ['student-history', id] }); qc.invalidateQueries({ queryKey: ['students'] }) }
  const archive = useMutation({ mutationFn: (a: boolean) => api.post(`/students/${id}/${a ? 'archive' : 'restore'}`), onSuccess: refresh })

  if (card.isLoading) return <Spinner />
  if (card.error || !card.data) return <div className="p-10 text-center text-muted">{tr('Ученик не найден')}</div>
  const c = card.data
  const s = c.student
  const courseIds = s.groups.map((g) => g.course_id)
  const fields = c.fields.filter((f) => inScope(f, courseIds))
  const grouped = groupByBlock(fields, blocks.data)
  const sub = c.subscriptions?.find((x) => x.status === 'active' || x.status === 'frozen') ?? c.subscriptions?.[0]
  const balance = s.balance_tiyn ?? 0
  const fieldLabel = (k: string) => {
    if (k.startsWith('cf.')) { const f = c.fields.find((x) => x.key === k.slice(3)); return f ? label(f.label) : k.slice(3) }
    return ({ full_name: tr('ФИО'), phone: tr('Телефон'), email: 'Email', status: tr('Статус'), birth_date: tr('Дата рождения'), iin: tr('ИИН'), source: tr('Источник'), branch_id: tr('Филиал'), responsible_id: tr('Ответственный'), comment: tr('Комментарий'), address: tr('Адрес'), gender: tr('Пол') } as Record<string, string>)[k] ?? k
  }
  const valueText = (k: string, v: unknown): string => {
    if (v == null || v === '') return '—'
    if (k === 'status') return studentStatus[String(v)]?.[0] ?? String(v)
    if (k.startsWith('cf.')) {
      const f = c.fields.find((x) => x.key === k.slice(3))
      if (f?.field_type === 'select') return label(f.options.find((o) => o.id === v)?.label) || String(v)
      if (f?.field_type === 'multiselect') return (v as string[]).map((x) => label(f.options.find((o) => o.id === x)?.label)).join(', ')
      if (f?.field_type === 'boolean') return v ? tr('Да') : tr('Нет')
    }
    return String(v)
  }
  const describe = (h: HistoryItem): string => {
    switch (h.action) {
      case 'update': return Object.keys(h.new ?? {}).map((k) => `${fieldLabel(k)}: ${valueText(k, h.old?.[k])} → ${valueText(k, h.new?.[k])}`).join('; ')
      case 'create': return h.new?.from_lead ? tr('Создан из лида') : tr('Карточка создана')
      case 'attendance_changed': return tr('Посещаемость{0}: {1} → {2}{3}', h.old?.starts_at ? ' ' + dateShort(String(h.old.starts_at)) : '', attendanceStatus[String(h.old?.status)]?.[0] ?? h.old?.status, attendanceStatus[String(h.new?.status)]?.[0] ?? h.new?.status, h.new?.reason ? tr(' · причина: ') + h.new.reason : '')
      case 'payment': return tr('Платёж {0} · {1}', money(Number(h.new?.amount_tiyn)), paymentMethods[String(h.new?.method)] ?? '')
      case 'refund': return tr('Возврат {0} · {1}', money(Number(h.new?.amount_tiyn)), h.new?.reason ?? '')
      case 'subscription_created': return tr('Абонемент «{0}» · {1}', h.new?.name, money(Number(h.new?.price_tiyn)))
      case 'subscription_frozen': return tr('Заморозка {0} – {1}{2}', date(String(h.new?.from)), date(String(h.new?.to)), h.new?.reason ? ' · ' + h.new.reason : '')
      case 'subscription_cancelled': return tr('Абонемент отменён')
      case 'group_joined': return tr('Добавлен в группу')
      case 'group_left': return tr('Выбыл из группы{0}', h.new?.reason ? ' · ' + h.new.reason : '')
      case 'parent_linked': return tr('Привязан родитель')
      case 'parent_unlinked': return tr('Отвязан родитель')
      case 'archive': return tr('Перемещён в архив')
      case 'restore': return tr('Восстановлен из архива')
      case 'merge': return tr('Объединён с дубликатом')
      case 'balance_adjusted': return tr('Корректировка баланса {0} · {1}', money(Number(h.new?.amount_tiyn), { sign: true }), h.new?.comment ?? '')
      case 'import': case 'import_update': return tr('Импорт из файла')
      case 'export': return tr('Экспорт')
    }
    return h.action
  }
  const reveal = async () => {
    try { setIin((await api.post<{ value: string }>(`/students/${id}/reveal`, { field: 'iin' })).value) } catch (e) { setToast((e as Error).message) }
  }

  return (
    <div className="mx-auto max-w-[1400px] p-4 lg:p-8">
      <div className="text-[13px] text-muted"><Link to="/students" className="text-brand-600 hover:underline">{tr('Ученики')}</Link> / {s.full_name}</div>
      <div className="mt-3 flex flex-wrap items-start gap-4">
        <Avatar name={s.full_name} size="lg" />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2.5">
            <h1 className="text-[26px] font-bold">{s.full_name}</h1>
            <StatusBadge map={studentStatus} value={s.status} />
            {balance < 0 && <Badge tone="orange">{tr('Долг')} {money(-balance)}</Badge>}
          </div>
          <div className="mt-1 text-sm text-muted tabular">
            ID {s.number}{s.phone && ` · ${phone(s.phone)}`}{c.details.started_at && tr(' · с {0}', new Date(c.details.started_at).toLocaleDateString(intlLocale(), { day: 'numeric', month: 'long', year: 'numeric' }))} · {c.details.branch_name}
          </div>
        </div>
        <div className="flex gap-2">
          {c.parents[0]?.phone && <a href={`https://wa.me/${c.parents[0].phone.replace('+', '')}`} target="_blank" rel="noreferrer"><Button>{tr('Написать')}</Button></a>}
          <Button disabled title={tr('Договоры и документы по шаблонам — этап 6 roadmap')}>{tr('Документ')}</Button>
          {can('payments.create') && <Button variant="primary" onClick={() => setModal('payment')}>{tr('Принять оплату')}</Button>}
          <div className="relative">
            <Button className="!px-2.5" onClick={() => setMenu(!menu)} aria-label={tr('Ещё')}><MoreHorizontal className="size-4" /></Button>
            {menu && (
              <Dropdown onClose={() => setMenu(false)}>
                {can('students.edit') && <MenuItem onClick={() => { setMenu(false); setModal('edit') }}>{tr('Редактировать')}</MenuItem>}
                {can('subscriptions.edit') && <MenuItem onClick={() => { setMenu(false); setModal('subscription') }}>{tr('Новый абонемент')}</MenuItem>}
                {can('payments.refund') && <MenuItem onClick={() => { setMenu(false); setModal('adjust') }}>{tr('Корректировка баланса')}</MenuItem>}
                {can('students.edit') && (s.status === 'archived'
                  ? <MenuItem onClick={() => { setMenu(false); archive.mutate(false) }}>{tr('Восстановить из архива')}</MenuItem>
                  : <MenuItem danger onClick={() => { setMenu(false); if (confirm(tr('Архивировать ученика? Он выйдет из групп, история сохранится.'))) archive.mutate(true) }}>{tr('Архивировать')}</MenuItem>)}
              </Dropdown>
            )}
          </div>
        </div>
      </div>

      <Tabs className="mt-6" value={tab} onChange={setTab} items={[
        { value: 'overview', label: tr('Обзор') }, { value: 'attendance', label: tr('Посещаемость') }, { value: 'tests', label: tr('Тесты') },
        ...(can('finance.view') ? [{ value: 'finance' as const, label: tr('Финансы') }] : []),
        { value: 'documents', label: tr('Документы') }, { value: 'history', label: tr('История') },
      ]} />

      {tab === 'overview' && (
        <div className="mt-6 grid gap-6 xl:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)]">
          <div className="space-y-5">
            <Card title={tr('Основное')} action={can('students.edit') && <Button variant="ghost" size="sm" onClick={() => setModal('edit')}>{tr('Изменить')}</Button>}>
              <dl className="grid gap-x-8 gap-y-4 sm:grid-cols-2">
                <Item k={tr('Дата рождения')} v={s.birth_date ? `${date(s.birth_date)} · ${age(s.birth_date)}` : '—'} />
                <Item k="Email" v={s.email || '—'} />
                <Item k={tr('Источник')} v={s.source || '—'} />
                <Item k={tr('Ответственный')} v={c.details.responsible_name || '—'} />
                {c.details.comment && <Item k={tr('Комментарий')} v={c.details.comment} wide />}
              </dl>
            </Card>
            {grouped.map(({ block, fields: fs }) => (
              <Card key={block?.id ?? 'other'} title={<span className="flex items-center gap-2">{block ? label(block.name) : tr('Дополнительно')}<span className="rounded bg-brand-50 px-1.5 py-0.5 text-[11px] font-medium text-brand-700">{tr('доп. поля')}</span></span>}
                action={can('students.edit') && <Button variant="ghost" size="sm" onClick={() => setModal({ block: block?.id ?? null })}>{tr('Изменить')}</Button>}>
                <dl className="grid gap-x-8 gap-y-4 sm:grid-cols-2">
                  {fs.map((f) => <Item key={f.key} k={label(f.label)} v={cfDisplay(f, s.custom_fields[f.key])} />)}
                  {block && block.name?.ru === 'Для договора' && (
                    <Item k={tr('ИИН')} v={s.iin ? <span className="flex items-center gap-2 tabular">{iin ?? s.iin}{!iin && can('students.view_iin') && <Button size="sm" className="!h-7" onClick={reveal}>{tr('Показать')}</Button>}</span> : '—'} />
                  )}
                  {block && block.name?.ru === 'Для договора' && <Item k={tr('Плательщик')} v={c.parents.find((p) => p.is_payer) ? `${c.parents.find((p) => p.is_payer)!.full_name} (${relationText[c.parents.find((p) => p.is_payer)!.relation]})` : '—'} />}
                </dl>
              </Card>
            ))}
            {!grouped.some((g) => g.block && g.block.name?.ru === 'Для договора') && s.iin && (
              <Card title={tr('Для договора')}><dl className="grid gap-4 sm:grid-cols-2"><Item k={tr('ИИН')} v={<span className="flex items-center gap-2 tabular">{iin ?? s.iin}{!iin && can('students.view_iin') && <Button size="sm" className="!h-7" onClick={reveal}>{tr('Показать')}</Button>}</span>} /></dl></Card>
            )}
            <Card title={tr('Родители и контакты')} action={can('students.edit') && <Button variant="ghost" size="sm" onClick={() => setModal('parent')}>{tr('+ Добавить')}</Button>} bodyClass="!pb-2">
              {c.parents.length === 0 && <div className="py-3 text-sm text-muted">{tr('Контакты не добавлены')}</div>}
              <ul className="divide-y divide-line">
                {c.parents.map((p) => (
                  <li key={p.id} className="flex flex-wrap items-center justify-between gap-3 py-3.5">
                    <div>
                      <div className="text-sm font-medium">{p.full_name} · {relationText[p.relation] ?? p.relation}</div>
                      <div className="text-[13px] text-muted">{phone(p.phone)}{p.is_payer && tr(' · плательщик')}{p.is_legal_rep && tr(' · законный представитель')}{!p.consent_at && tr(' · нет согласия на обработку ПД')}</div>
                    </div>
                    {p.telegram_linked ? <Badge tone="blue">{tr('Telegram привязан')}</Badge> : p.telegram_link && (
                      <Button size="sm" onClick={() => { navigator.clipboard?.writeText(p.telegram_link ?? '');setToast(tr('Ссылка на бота скопирована — отправьте её родителю')) }}>{tr('Отправить ссылку на бота')}</Button>
                    )}
                  </li>
                ))}
              </ul>
            </Card>
          </div>

          <div className="space-y-5">
            {can('finance.view') && (
              <Card title={tr('Абонемент и баланс')}>
                <div className="flex items-center justify-between"><span className="text-sm text-muted">{tr('Баланс')}</span><span className={cx('text-[22px] font-bold tabular', balance < 0 ? 'text-red-700' : 'text-ink')}>{money(balance)}</span></div>
                {sub ? (
                  <div className="mt-3 rounded-xl bg-gray-50 p-4">
                    <div className="flex justify-between gap-3 text-sm font-medium"><span>{sub.name}</span><span className="tabular">{money(sub.price_tiyn)}</span></div>
                    {sub.lessons_total && <div className="mt-2.5 h-1.5 overflow-hidden rounded-full bg-gray-200"><div className="h-full bg-brand-600" style={{ width: `${Math.min(100, ((sub.lessons_total - sub.lessons_used) / sub.lessons_total) * 100)}%` }} /></div>}
                    <div className="mt-2 flex justify-between text-[13px] text-muted">
                      <span>{sub.lessons_total ? tr('Осталось {0} из {1}', sub.lessons_total - sub.lessons_used, sub.lessons_total) : sub.name}{sub.status === 'frozen' && tr(' · заморожен')}{sub.status === 'used' && tr(' · израсходован')}</span>
                      {sub.ends_on && <span>{tr('до')} {date(sub.ends_on)}</span>}
                    </div>
                  </div>
                ) : <div className="mt-3 rounded-xl bg-gray-50 p-4 text-sm text-muted">{tr('Абонемент не оформлен')}</div>}
                {can('subscriptions.edit') && (
                  <div className="mt-3 grid grid-cols-2 gap-2">
                    <Button disabled={!sub || !['active', 'frozen'].includes(sub.status)} onClick={() => setModal('freeze')}>{tr('Заморозить')}</Button>
                    <Button onClick={() => setModal('subscription')}>{tr('Продлить')}</Button>
                  </div>
                )}
              </Card>
            )}
            <Card title={tr('Группы')}>
              {c.groups.length === 0 ? <div className="text-sm text-muted">{tr('Не состоит в группах')}</div> : c.groups.map((g) => (
                <Link key={g.id} to={`/groups/${g.id}`} className="block py-1.5 hover:text-brand-600">
                  <div className="text-sm font-medium">{g.name}{g.type === 'individual' && tr(' · индивидуально')}</div>
                  <div className="text-[13px] text-muted">{g.schedule && g.schedule.split('; ').map((x) => { const [d, t] = x.split(' '); return d.split(',').map((n) => days[+n]).join(', ') + ' ' + t }).join('; ')}{g.teacher && ` · ${g.teacher}`}{g.classroom && ` · ${g.classroom}`}</div>
                </Link>
              ))}
            </Card>
            <Card title={tr('Посещаемость')} action={c.attendance.rate != null && <span className="text-[15px] font-semibold tabular">{c.attendance.rate}%</span>}>
              <div className="flex flex-wrap gap-1.5">
                {c.attendance.recent.length === 0 && <span className="text-sm text-muted">{tr('Отметок пока нет')}</span>}
                {c.attendance.recent.map((a, i) => {
                  const [l, short, tone] = attendanceStatus[a.status] ?? [a.status, '?', 'gray']
                  return <Badge key={i} tone={tone} className="!rounded-md !px-1.5 tabular"><span title={`${date(a.date)} · ${l}${a.reason ? ' · ' + a.reason : ''}`}>{short}</span></Badge>
                })}
              </div>
              <div className="mt-3 text-xs text-muted">{tr('П — был · О — опоздал · Н — не был · У — уважительная')}</div>
            </Card>
            <Card title={tr('Последние изменения')} bodyClass="!pb-2">
              <ul className="divide-y divide-line">
                {(history.data ?? []).slice(0, 6).map((h) => (
                  <li key={h.id} className="py-3">
                    <div className="text-sm">{describe(h)}</div>
                    <div className="mt-0.5 text-xs text-muted">{h.user} · {dateTime(h.created_at)}</div>
                  </li>
                ))}
              </ul>
            </Card>
          </div>
        </div>
      )}

      {tab === 'attendance' && <AttendanceTab id={id!} />}
      {tab === 'tests' && <StudentTests studentId={id!} groups={c.groups} />}
      {tab === 'finance' && <FinanceTab id={id!} subs={c.subscriptions ?? []} onChange={refresh} />}
      {tab === 'documents' && <Card className="mt-6"><div className="py-10 text-center text-sm text-muted">{tr('Генерация договоров и квитанций по шаблонам с подстановкой полей (включая пользовательские) — этап 6 roadmap.')}</div></Card>}
      {tab === 'history' && (
        <Card className="mt-6" bodyClass="!py-2">
          <ul className="divide-y divide-line">
            {(history.data ?? []).map((h) => <li key={h.id} className="flex justify-between gap-4 py-3"><span className="text-sm">{describe(h)}</span><span className="shrink-0 text-xs text-muted">{h.user} · {dateTime(h.created_at)}</span></li>)}
          </ul>
        </Card>
      )}

      {modal === 'edit' && <EditModal card={c} onClose={() => setModal(null)} onSaved={() => { setModal(null); refresh() }} />}
      {modal && typeof modal === 'object' && 'block' in modal && <EditCFModal card={c} fields={fields.filter((f) => (f.block_id ?? null) === modal.block || (!modal.block && !blocks.data?.some((b) => b.id === f.block_id)))} onClose={() => setModal(null)} onSaved={() => { setModal(null); refresh() }} />}
      {modal === 'payment' && <PaymentModal studentId={id} onClose={() => { setModal(null); refresh() }} />}
      {modal === 'subscription' && <SubscriptionModal studentId={id!} courseId={s.groups[0]?.course_id} onClose={() => { setModal(null); refresh() }} />}
      {modal === 'freeze' && sub && <FreezeModal subscriptionId={sub.id} onClose={() => { setModal(null); refresh() }} />}
      {modal === 'parent' && <ParentModal studentId={id!} onClose={() => { setModal(null); refresh() }} />}
      {modal === 'adjust' && <AdjustModal studentId={id!} onClose={() => { setModal(null); refresh() }} />}
      {toast && <Toast text={toast} onDone={() => setToast('')} />}
    </div>
  )
}

const Item = ({ k, v, wide }: { k: string; v: React.ReactNode; wide?: boolean }) => (
  <div className={wide ? 'sm:col-span-2' : ''}><dt className="text-[13px] text-muted">{k}</dt><dd className="mt-1 text-sm">{v}</dd></div>
)

function EditModal({ card, onClose, onSaved }: { card: Card; onClose: () => void; onSaved: () => void }) {
  const s = card.student
  const { me } = useAuth()
  const [f, setF] = useState({ full_name: s.full_name, phone: s.phone ?? '', email: s.email ?? '', birth_date: s.birth_date?.slice(0, 10) ?? '', status: s.status, source: s.source ?? '', comment: card.details.comment ?? '', branch_id: s.branch_id, iin: '' })
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [conflict, setConflict] = useState(false)
  const m = useMutation({
    mutationFn: () => api.patch(`/students/${s.id}`, { ...f, iin: f.iin || undefined, version: card.details.version }),
    onSuccess: onSaved,
    onError: (e) => { setErrors(errorsByField(e)); setConflict(e instanceof ApiError && e.code === 'VERSION_CONFLICT') },
  })
  return (
    <Modal open wide title={tr('Редактирование')} onClose={onClose} footer={<><Button onClick={onClose}>{tr('Отмена')}</Button><Button variant="primary" loading={m.isPending} onClick={() => m.mutate()}>{tr('Сохранить')}</Button></>}>
      {conflict && <div className="mb-4"><Banner tone="red">{tr('Карточку уже изменил другой пользователь. Закройте окно и обновите страницу.')}</Banner></div>}
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={tr('ФИО')} className="sm:col-span-2" error={errors.full_name}><Input value={f.full_name} onChange={(e) => setF({ ...f, full_name: e.target.value })} /></Field>
        <Field label={tr('Телефон')} error={errors.phone}><Input value={f.phone} onChange={(e) => setF({ ...f, phone: e.target.value })} /></Field>
        <Field label="Email" error={errors.email}><Input value={f.email} onChange={(e) => setF({ ...f, email: e.target.value })} /></Field>
        <Field label={tr('Дата рождения')} error={errors.birth_date}><Input type="date" value={f.birth_date} onChange={(e) => setF({ ...f, birth_date: e.target.value })} /></Field>
        <Field label={tr('Статус')} error={errors.status}><Select value={f.status} onChange={(e) => setF({ ...f, status: e.target.value })}>{Object.entries(studentStatus).map(([k, [l]]) => <option key={k} value={k}>{l}</option>)}</Select></Field>
        <Field label={tr('Филиал')}><Select value={f.branch_id} onChange={(e) => setF({ ...f, branch_id: e.target.value })}>{me?.branches?.map((b) => <option key={b.id} value={b.id}>{b.name}</option>)}</Select></Field>
        <Field label={tr('Источник')}><Input value={f.source} onChange={(e) => setF({ ...f, source: e.target.value })} /></Field>
        <Field label={tr('ИИН')} hint={tr('Оставьте пустым, чтобы не менять. 12 цифр с контрольной цифрой.')} error={errors.iin}><Input inputMode="numeric" maxLength={12} value={f.iin} onChange={(e) => setF({ ...f, iin: e.target.value.replace(/\D/g, '') })} /></Field>
        <Field label={tr('Комментарий')} className="sm:col-span-2"><Textarea value={f.comment} onChange={(e) => setF({ ...f, comment: e.target.value })} /></Field>
      </div>
      {Object.keys(errors).some((k) => k.startsWith('custom_fields.')) && <div className="mt-3 text-sm text-red-700">{tr('Для статуса «')}{studentStatus[f.status][0]}{tr('» нужно заполнить обязательные доп. поля.')}</div>}
    </Modal>
  )
}

function EditCFModal({ card, fields, onClose, onSaved }: { card: Card; fields: CField[]; onClose: () => void; onSaved: () => void }) {
  const [cf, setCf] = useState<Record<string, unknown>>(() => Object.fromEntries(fields.map((f) => [f.key, card.student.custom_fields[f.key] ?? null])))
  const [errors, setErrors] = useState<Record<string, string>>({})
  const m = useMutation({
    mutationFn: () => api.patch(`/students/${card.student.id}`, { custom_fields: cf, version: card.details.version }),
    onSuccess: onSaved,
    onError: (e) => { setErrors(errorsByField(e)); if (e instanceof ApiError && !e.fields.length) setErrors({ _: e.message }) },
  })
  return (
    <Modal open wide title={tr('Дополнительные поля')} onClose={onClose} footer={<><Button onClick={onClose}>{tr('Отмена')}</Button><Button variant="primary" loading={m.isPending} onClick={() => m.mutate()}>{tr('Сохранить')}</Button></>}>
      <div className="grid gap-4 sm:grid-cols-2">
        {fields.map((f) => <CFInput key={f.key} field={f} value={cf[f.key]} onChange={(v) => setCf({ ...cf, [f.key]: v })} error={errors['custom_fields.' + f.key]}
          required={f.settings.required === 'always' || (f.settings.required === 'on_status' && f.settings.required_status === card.student.status)} />)}
      </div>
      {errors._ && <div className="mt-3 text-sm text-red-700">{errors._}</div>}
    </Modal>
  )
}

function ParentModal({ studentId, onClose }: { studentId: string; onClose: () => void }) {
  const [f, setF] = useState({ full_name: '', phone: '', email: '', relation: 'mother', is_payer: false, is_legal_rep: false, consent: true })
  const [errors, setErrors] = useState<Record<string, string>>({})
  const m = useMutation({ mutationFn: () => api.post(`/students/${studentId}/parents`, f), onSuccess: onClose, onError: (e) => setErrors(errorsByField(e)) })
  return (
    <Modal open title={tr('Добавить родителя / контакт')} onClose={onClose} footer={<><Button onClick={onClose}>{tr('Отмена')}</Button><Button variant="primary" loading={m.isPending} onClick={() => m.mutate()}>{tr('Добавить')}</Button></>}>
      <div className="space-y-4">
        <Field label={tr('ФИО')} error={errors.parents}><Input value={f.full_name} onChange={(e) => setF({ ...f, full_name: e.target.value })} /></Field>
        <div className="grid grid-cols-2 gap-4">
          <Field label={tr('Телефон')}><Input value={f.phone} onChange={(e) => setF({ ...f, phone: e.target.value })} placeholder="+7 7__ ___-__-__" /></Field>
          <Field label={tr('Кем приходится')}><Select value={f.relation} onChange={(e) => setF({ ...f, relation: e.target.value })}><option value="mother">{tr('Мама')}</option><option value="father">{tr('Папа')}</option><option value="guardian">{tr('Опекун')}</option><option value="other">{tr('Другое')}</option></Select></Field>
        </div>
        <Checkbox checked={f.is_payer} onChange={(v) => setF({ ...f, is_payer: v })} label={tr('Плательщик')} />
        <Checkbox checked={f.is_legal_rep} onChange={(v) => setF({ ...f, is_legal_rep: v })} label={tr('Законный представитель')} />
        <Checkbox checked={f.consent} onChange={(v) => setF({ ...f, consent: v })} label={tr('Получено согласие на обработку персональных данных')} />
        <p className="text-xs text-muted">{tr('Если родитель с таким телефоном уже есть в базе — он будет привязан, а не создан повторно.')}</p>
      </div>
    </Modal>
  )
}

function AdjustModal({ studentId, onClose }: { studentId: string; onClose: () => void }) {
  const [amount, setAmount] = useState('')
  const [comment, setComment] = useState('')
  const [err, setErr] = useState('')
  const m = useMutation({ mutationFn: () => api.post(`/students/${studentId}/adjust`, { amount_tiyn: Math.round(Number(amount.replace(',', '.')) * 100), comment }), onSuccess: onClose, onError: (e) => setErr((e as Error).message) })
  return (
    <Modal open title={tr('Корректировка баланса')} onClose={onClose} footer={<><Button onClick={onClose}>{tr('Отмена')}</Button><Button variant="primary" loading={m.isPending} onClick={() => m.mutate()}>{tr('Провести')}</Button></>}>
      <p className="mb-4 text-sm text-muted">{tr('Проводки не редактируются и не удаляются — исправление делается корректирующей операцией с записью в историю.')}</p>
      <div className="space-y-4">
        <Field label={tr('Сумма, ₸ (минус — списать)')}><Input value={amount} onChange={(e) => setAmount(e.target.value)} placeholder="-6000" /></Field>
        <Field label={tr('Причина')}><Input value={comment} onChange={(e) => setComment(e.target.value)} /></Field>
        {err && <div className="text-sm text-red-700">{err}</div>}
      </div>
    </Modal>
  )
}

function AttendanceTab({ id }: { id: string }) {
  const card = useQuery({ queryKey: ['student', id], queryFn: () => api.get<Card>('/students/' + id) })
  const hist = useQuery({ queryKey: ['att-history', id], queryFn: () => api.get<{ changed_at: string; old: string; new: string; reason: string | null; user: string; group: string; lesson_at: string }[]>(`/attendance/history?student_id=${id}`) })
  return (
    <div className="mt-6 grid gap-6 lg:grid-cols-2">
      <Card title={tr('Последние занятия')}>
        <ul className="divide-y divide-line">
          {[...(card.data?.attendance.recent ?? [])].reverse().map((a, i) => {
            const [l, , tone] = attendanceStatus[a.status] ?? [a.status, '', 'gray']
            return <li key={i} className="flex items-center justify-between py-2.5 text-sm"><span>{dateTime(a.date)}</span><span className="flex items-center gap-2">{a.reason && <span className="text-xs text-muted">{a.reason}</span>}<Badge tone={tone}>{l}</Badge></span></li>
          })}
        </ul>
      </Card>
      <Card title={tr('Изменения отметок')}>
        {(hist.data ?? []).length === 0 ? <div className="text-sm text-muted">{tr('Отметки не исправлялись')}</div> : (
          <ul className="divide-y divide-line">
            {hist.data!.map((h, i) => <li key={i} className="py-2.5 text-sm"><div>{h.group}, {dateShort(h.lesson_at)}: {attendanceStatus[h.old]?.[0]} → {attendanceStatus[h.new]?.[0]}{h.reason && ` · ${h.reason}`}</div><div className="text-xs text-muted">{h.user} · {dateTime(h.changed_at)}</div></li>)}
          </ul>
        )}
      </Card>
    </div>
  )
}

function FinanceTab({ id, subs, onChange }: { id: string; subs: (Subscription & { lessons_used: number })[]; onChange: () => void }) {
  const { can } = useAuth()
  const ledger = useQuery({ queryKey: ['ledger', id], queryFn: () => api.getMeta<{ id: number; kind: string; amount_tiyn: number; comment: string | null; created_at: string; user: string; ref_type: string | null; ref_id: string | null }[]>(`/students/${id}/ledger`) })
  const payments = useQuery({ queryKey: ['payments', 'student', id], queryFn: () => api.get<{ id: string; amount_tiyn: number; paid_at: string; method: string; status: string; refunded_tiyn: number; received_by: string }[]>(`/payments?student_id=${id}&from=2000-01-01`) })
  const [refund, setRefund] = useState<string | null>(null)
  const kinds: Record<string, string> = { payment: tr('Платёж'), charge: tr('Начисление'), refund: tr('Возврат'), adjustment: tr('Корректировка') }
  const cancel = useMutation({ mutationFn: (sid: string) => api.post(`/subscriptions/${sid}/cancel`, { refund_unused: true }), onSuccess: () => { onChange(); ledger.refetch() } })
  return (
    <div className="mt-6 grid gap-6 xl:grid-cols-2">
      <Card title={tr('Проводки')} action={<span className="text-sm">{tr('Баланс:')} <b className="tabular">{money(ledger.data?.meta.balance_tiyn as number)}</b></span>} bodyClass="!px-0 !pb-2">
        <Table2>
          {ledger.data?.data.map((l) => (
            <tr key={l.id}><td className="px-5 py-2.5 text-[13px] text-muted">{dateTime(l.created_at)}</td><td className="px-3 py-2.5 text-sm">{kinds[l.kind]}<div className="text-xs text-muted">{l.comment}</div></td>
              <td className={cx('px-5 py-2.5 text-right text-sm font-semibold tabular', l.amount_tiyn < 0 ? 'text-red-700' : 'text-green-700')}>{money(l.amount_tiyn, { sign: true })}</td></tr>
          ))}
        </Table2>
      </Card>
      <div className="space-y-6">
        <Card title={tr('Абонементы')} bodyClass="!pb-2">
          <ul className="divide-y divide-line">
            {subs.map((s) => (
              <li key={s.id} className="flex items-center justify-between gap-3 py-3">
                <div><div className="text-sm font-medium">{s.name}</div><div className="text-xs text-muted">{date(s.starts_on)} – {date(s.ends_on)}{s.lessons_total && tr(' · использовано {0} из {1}', s.lessons_used, s.lessons_total)}</div></div>
                <div className="flex items-center gap-2">
                  <Badge tone={{ active: 'green', frozen: 'blue', used: 'gray', expired: 'gray', cancelled: 'red' }[s.status] as never}>{{ active: tr('Активен'), frozen: tr('Заморожен'), used: tr('Израсходован'), expired: tr('Истёк'), cancelled: tr('Отменён') }[s.status]}</Badge>
                  {can('subscriptions.edit') && ['active', 'frozen'].includes(s.status) && <Button size="sm" onClick={() => confirm(tr('Отменить абонемент и вернуть неиспользованные занятия на баланс?')) && cancel.mutate(s.id)}>{tr('Отменить')}</Button>}
                </div>
              </li>
            ))}
          </ul>
        </Card>
        <Card title={tr('Платежи')} bodyClass="!pb-2">
          <ul className="divide-y divide-line">
            {payments.data?.map((p) => (
              <li key={p.id} className="flex items-center justify-between gap-3 py-3">
                <div><div className="text-sm font-medium tabular">{money(p.amount_tiyn)} · {paymentMethods[p.method]}</div><div className="text-xs text-muted">{dateTime(p.paid_at)} · {p.received_by}{p.refunded_tiyn > 0 && tr(' · возвращено {0}', money(p.refunded_tiyn))}</div></div>
                {can('payments.refund') && p.status !== 'refunded' && <Button size="sm" onClick={() => setRefund(p.id)}>{tr('Возврат')}</Button>}
              </li>
            ))}
          </ul>
        </Card>
      </div>
      {refund && <RefundModal paymentId={refund} onClose={() => { setRefund(null); onChange(); ledger.refetch(); payments.refetch() }} />}
    </div>
  )
}

const Table2 = ({ children }: { children: React.ReactNode }) => <table className="w-full"><tbody className="divide-y divide-line">{children}</tbody></table>

function RefundModal({ paymentId, onClose }: { paymentId: string; onClose: () => void }) {
  const [amount, setAmount] = useState('')
  const [reason, setReason] = useState('')
  const [errors, setErrors] = useState<Record<string, string>>({})
  const m = useMutation({ mutationFn: () => api.post(`/payments/${paymentId}/refund`, { amount_tiyn: Math.round(Number(amount.replace(/\s/g, '').replace(',', '.')) * 100), reason }), onSuccess: onClose, onError: (e) => setErrors(errorsByField(e)) })
  return (
    <Modal open title={tr('Возврат платежа')} onClose={onClose} footer={<><Button onClick={onClose}>{tr('Отмена')}</Button><Button variant="danger" loading={m.isPending} onClick={() => m.mutate()}>{tr('Оформить возврат')}</Button></>}>
      <div className="space-y-4">
        <Field label={tr('Сумма возврата, ₸')} error={errors.amount_tiyn}><Input value={amount} onChange={(e) => setAmount(e.target.value)} /></Field>
        <Field label={tr('Причина')} error={errors.reason}><Input value={reason} onChange={(e) => setReason(e.target.value)} /></Field>
      </div>
    </Modal>
  )
}
