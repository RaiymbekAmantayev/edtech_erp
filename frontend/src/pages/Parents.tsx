import { useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, errorsByField } from '../lib/api'
import { useAuth } from '../lib/auth'
import { label } from '../lib/i18n'
import { date, phone } from '../lib/format'
import type { CField } from '../lib/types'
import { Badge, Button, Card, Checkbox, Drawer, Field, Input, Select, Spinner, Table, Toast } from '../components/ui'
import { CFInput, cfDisplay } from '../components/CustomFields'

type P = { id: string; full_name: string; phone: string | null; email: string | null; telegram_linked: boolean; telegram_link?: string; preferred_channel: string; locale: string; consent_at: string | null; comment: string | null; custom_fields: Record<string, unknown>; students: { id: string; full_name: string; relation: string; is_payer: boolean; status: string }[] }

export default function Parents() {
  const [params, setParams] = useSearchParams()
  const [q, setQ] = useState('')
  const list = useQuery({ queryKey: ['parents', q], queryFn: () => api.getMeta<P[]>(`/parents?limit=100&q=${encodeURIComponent(q)}`) })
  const open = params.get('open')
  return (
    <div className="mx-auto max-w-[1300px] p-4 lg:p-8">
      <h1 className="text-[26px] font-bold">Родители <span className="text-lg font-medium text-muted tabular">{list.data?.meta.total as number}</span></h1>
      <Input className="mt-5 !h-9 max-w-sm" placeholder="ФИО или телефон" value={q} onChange={(e) => setQ(e.target.value)} />
      <Card className="mt-3" bodyClass="!p-0">
        {list.isLoading ? <Spinner /> : (
          <Table head={['Родитель', 'Дети', 'Telegram', 'Согласие на ПД', 'Канал']}>
            {list.data?.data.map((p) => (
              <tr key={p.id} className="cursor-pointer hover:bg-gray-50" onClick={() => setParams({ open: p.id })}>
                <td className="px-4 py-3"><div className="font-medium">{p.full_name}</div><div className="text-xs text-muted">{phone(p.phone)}</div></td>
                <td className="px-4 py-3 text-[13px]">{p.students.map((s) => s.full_name).join(', ') || '—'}</td>
                <td className="px-4 py-3">{p.telegram_linked ? <Badge tone="blue">привязан</Badge> : <span className="text-[13px] text-muted">нет</span>}</td>
                <td className="px-4 py-3 text-[13px]">{p.consent_at ? date(p.consent_at) : <span className="text-orange-700">не получено</span>}</td>
                <td className="px-4 py-3 text-[13px]">{p.preferred_channel}</td>
              </tr>
            ))}
          </Table>
        )}
      </Card>
      {open && <ParentDrawer id={open} onClose={() => setParams({})} />}
    </div>
  )
}

function ParentDrawer({ id, onClose }: { id: string; onClose: () => void }) {
  const { can } = useAuth()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['parent', id], queryFn: () => api.get<{ parent: P; fields: CField[] }>('/parents/' + id) })
  const [edit, setEdit] = useState<Record<string, unknown> | null>(null)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [toast, setToast] = useState('')
  const m = useMutation({
    mutationFn: () => api.patch('/parents/' + id, edit),
    onSuccess: () => { setEdit(null); qc.invalidateQueries({ queryKey: ['parent', id] }); qc.invalidateQueries({ queryKey: ['parents'] }) },
    onError: (e) => setErrors(errorsByField(e)),
  })
  if (!q.data) return <Drawer open title="Родитель" onClose={onClose}><Spinner /></Drawer>
  const { parent: p, fields } = q.data
  return (
    <Drawer open title={p.full_name} onClose={onClose}>
      {!edit ? (
        <div className="space-y-5 text-sm">
          <dl className="grid grid-cols-2 gap-4">
            <div><dt className="text-muted">Телефон</dt><dd>{phone(p.phone) || '—'}</dd></div>
            <div><dt className="text-muted">Email</dt><dd>{p.email || '—'}</dd></div>
            <div><dt className="text-muted">Предпочтительный канал</dt><dd>{p.preferred_channel}</dd></div>
            <div><dt className="text-muted">Язык уведомлений</dt><dd>{{ ru: 'Русский', kk: 'Қазақша', en: 'English' }[p.locale]}</dd></div>
            <div><dt className="text-muted">Согласие на обработку ПД</dt><dd>{p.consent_at ? date(p.consent_at) : 'не получено'}</dd></div>
            <div><dt className="text-muted">Telegram</dt><dd>{p.telegram_linked ? 'привязан' : <button className="text-brand-600" onClick={() => { navigator.clipboard?.writeText(p.telegram_link ?? ''); setToast('Ссылка скопирована') }}>скопировать ссылку на бота</button>}</dd></div>
            {fields.map((f) => <div key={f.key}><dt className="text-muted">{label(f.label)}</dt><dd>{cfDisplay(f, p.custom_fields[f.key])}</dd></div>)}
          </dl>
          <div>
            <div className="mb-2 font-semibold">Дети</div>
            {p.students.map((s) => <Link key={s.id} to={`/students/${s.id}`} className="block py-1 text-brand-600 hover:underline">{s.full_name}{s.is_payer && ' · плательщик'}</Link>)}
          </div>
          {can('parents.edit') && <Button onClick={() => setEdit({ full_name: p.full_name, phone: p.phone ?? '', email: p.email ?? '', preferred_channel: p.preferred_channel, locale: p.locale, custom_fields: { ...p.custom_fields } })}>Редактировать</Button>}
        </div>
      ) : (
        <div className="space-y-4">
          <Field label="ФИО" error={errors.full_name}><Input value={edit.full_name as string} onChange={(e) => setEdit({ ...edit, full_name: e.target.value })} /></Field>
          <Field label="Телефон" error={errors.phone}><Input value={edit.phone as string} onChange={(e) => setEdit({ ...edit, phone: e.target.value })} /></Field>
          <Field label="Email" error={errors.email}><Input value={edit.email as string} onChange={(e) => setEdit({ ...edit, email: e.target.value })} /></Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Канал"><Select value={edit.preferred_channel as string} onChange={(e) => setEdit({ ...edit, preferred_channel: e.target.value })}><option value="telegram">Telegram</option><option value="whatsapp">WhatsApp</option><option value="sms">SMS</option></Select></Field>
            <Field label="Язык"><Select value={edit.locale as string} onChange={(e) => setEdit({ ...edit, locale: e.target.value })}><option value="ru">Русский</option><option value="kk">Қазақша</option><option value="en">English</option></Select></Field>
          </div>
          {fields.map((f) => <CFInput key={f.key} field={f} value={(edit.custom_fields as Record<string, unknown>)[f.key]} error={errors['custom_fields.' + f.key]}
            onChange={(v) => setEdit({ ...edit, custom_fields: { ...(edit.custom_fields as object), [f.key]: v } })} />)}
          {!p.consent_at && <Checkbox checked={!!edit.consent} onChange={(v) => setEdit({ ...edit, consent: v })} label="Получено согласие на обработку персональных данных" />}
          <div className="flex gap-2"><Button onClick={() => setEdit(null)}>Отмена</Button><Button variant="primary" loading={m.isPending} onClick={() => m.mutate()}>Сохранить</Button></div>
        </div>
      )}
      {toast && <Toast text={toast} onDone={() => setToast('')} />}
    </Drawer>
  )
}
