import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, errorsByField } from '../lib/api'
import { useAuth } from '../lib/auth'
import { label } from '../lib/i18n'
import type { Group } from '../lib/types'
import { Banner, Button, Checkbox, Field, Input, Modal, Select, studentStatus } from '../components/ui'
import { CFInput, inScope, useFields } from '../components/CustomFields'

export function StudentFormModal({ onClose, onCreated }: { onClose: () => void; onCreated: (id: string) => void }) {
  const { me, branchId } = useAuth()
  const qc = useQueryClient()
  const fieldsQ = useFields('student')
  const groups = useQuery({ queryKey: ['groups', 'all-active'], queryFn: () => api.get<Group[]>('/groups') })
  const [f, setF] = useState({ full_name: '', phone: '', email: '', birth_date: '', branch_id: branchId || me?.branches?.[0]?.id || '', status: 'active', group_id: '', source: '' })
  const [parent, setParent] = useState({ full_name: '', phone: '', relation: 'mother', consent: true })
  const [cf, setCf] = useState<Record<string, unknown>>({})
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [dups, setDups] = useState<{ kind: string; full_name: string }[]>([])

  useEffect(() => {
    const digits = f.phone.replace(/\D/g, '')
    if (digits.length < 10) { setDups([]); return }
    const t = setTimeout(() => api.get(`/students/duplicates?phone=${encodeURIComponent(f.phone)}`).then(setDups).catch(() => {}), 400)
    return () => clearTimeout(t)
  }, [f.phone])

  const group = groups.data?.find((g) => g.id === f.group_id)
  const courseIds = group ? [group.course_id] : []
  const fields = (fieldsQ.data?.data ?? []).filter((x) => inScope(x, courseIds) && (x.settings.show_in_create || x.settings.required === 'always' || (x.settings.required === 'on_status' && x.settings.required_status === f.status)))
  const sources = ['Instagram', '2GIS', 'Сайт', 'Рекомендация', 'Звонок', 'WhatsApp']

  const m = useMutation({
    mutationFn: () => api.post<{ id: string }>('/students', {
      ...f, group_id: f.group_id || undefined, birth_date: f.birth_date || undefined, custom_fields: cf,
      parents: parent.full_name ? [{ ...parent, is_payer: true, is_legal_rep: true }] : [],
    }),
    onSuccess: (r) => { qc.invalidateQueries({ queryKey: ['students'] }); onCreated(r.id) },
    onError: (e) => setErrors(errorsByField(e)),
  })
  const err = (k: string) => errors[k] ?? errors['custom_fields.' + k]

  return (
    <Modal open wide title="Новый ученик" onClose={onClose}
      footer={<><Button onClick={onClose}>Отмена</Button><Button variant="primary" loading={m.isPending} onClick={() => { setErrors({}); m.mutate() }}>Создать</Button></>}>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="ФИО ученика" required error={err('full_name')} className="sm:col-span-2"><Input autoFocus value={f.full_name} onChange={(e) => setF({ ...f, full_name: e.target.value })} /></Field>
        <Field label="Телефон" error={err('phone')}><Input type="tel" value={f.phone} onChange={(e) => setF({ ...f, phone: e.target.value })} placeholder="+7 7__ ___-__-__" /></Field>
        <Field label="Дата рождения" error={err('birth_date')}><Input type="date" value={f.birth_date} onChange={(e) => setF({ ...f, birth_date: e.target.value })} /></Field>
        {dups.length > 0 && <div className="sm:col-span-2"><Banner tone="yellow">С этим телефоном уже есть: {dups.map((d) => `${d.full_name} (${{ student: 'ученик', parent: 'родитель', lead: 'лид' }[d.kind]})`).join(', ')}</Banner></div>}
        <Field label="Филиал" required error={err('branch_id')}>
          <Select value={f.branch_id} onChange={(e) => setF({ ...f, branch_id: e.target.value })}>{me?.branches?.map((b) => <option key={b.id} value={b.id}>{b.name}</option>)}</Select>
        </Field>
        <Field label="Статус">
          <Select value={f.status} onChange={(e) => setF({ ...f, status: e.target.value })}>{['registration', 'active'].map((s) => <option key={s} value={s}>{studentStatus[s][0]}</option>)}</Select>
        </Field>
        <Field label="Группа">
          <Select value={f.group_id} onChange={(e) => setF({ ...f, group_id: e.target.value })}>
            <option value="">— без группы —</option>
            {groups.data?.filter((g) => g.branch_id === f.branch_id).map((g) => <option key={g.id} value={g.id} disabled={!!g.capacity && g.students >= g.capacity}>{g.name} · {g.students}/{g.capacity ?? '∞'}</option>)}
          </Select>
        </Field>
        <Field label="Источник"><Select value={f.source} onChange={(e) => setF({ ...f, source: e.target.value })}><option value="">—</option>{sources.map((s) => <option key={s}>{s}</option>)}</Select></Field>
      </div>
      {fields.length > 0 && (
        <>
          <div className="mt-6 mb-3 text-sm font-semibold">Дополнительные поля <span className="ml-1 rounded bg-brand-50 px-1.5 py-0.5 text-xs font-medium text-brand-700">доп. поля</span></div>
          <div className="grid gap-4 sm:grid-cols-2">
            {fields.map((x) => <CFInput key={x.key} field={x} value={cf[x.key]} onChange={(v) => setCf({ ...cf, [x.key]: v })} error={err(x.key)}
              required={x.settings.required === 'always' || (x.settings.required === 'on_status' && x.settings.required_status === f.status)} />)}
          </div>
        </>
      )}
      <div className="mt-6 mb-3 text-sm font-semibold">Родитель / законный представитель</div>
      <div className="grid gap-4 sm:grid-cols-3">
        <Field label="ФИО" error={err('parents')}><Input value={parent.full_name} onChange={(e) => setParent({ ...parent, full_name: e.target.value })} /></Field>
        <Field label="Телефон"><Input type="tel" value={parent.phone} onChange={(e) => setParent({ ...parent, phone: e.target.value })} placeholder="+7 7__ ___-__-__" /></Field>
        <Field label="Кем приходится">
          <Select value={parent.relation} onChange={(e) => setParent({ ...parent, relation: e.target.value })}><option value="mother">Мама</option><option value="father">Папа</option><option value="guardian">Опекун</option><option value="other">Другое</option></Select>
        </Field>
      </div>
      <div className="mt-3"><Checkbox checked={parent.consent} onChange={(v) => setParent({ ...parent, consent: v })} label="Законный представитель дал согласие на обработку персональных данных несовершеннолетнего" /></div>
      {Object.keys(errors).length > 0 && <div className="mt-4 text-sm text-red-700">Проверьте поля: {Object.keys(errors).map((k) => { const key = k.replace('custom_fields.', ''); const fd = fieldsQ.data?.data.find((x) => x.key === key); return fd ? label(fd.label) : key }).join(', ')}</div>}
    </Modal>
  )
}
