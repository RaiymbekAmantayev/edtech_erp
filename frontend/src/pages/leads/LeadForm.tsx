import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, errorsByField } from '../../lib/api'
import { useAuth } from '../../lib/auth'
import type { Course } from '../../lib/types'
import { Banner, Button, Field, Input, Modal, Select, Textarea } from '../../components/ui'
import { CFInput, useFields } from '../../components/CustomFields'
import { tr } from '../../lib/i18n'

export const sources = ['Instagram', '2GIS', tr('Сайт'), tr('Рекомендация'), tr('Звонок'), 'WhatsApp', 'amoCRM']

export function LeadFormModal({ onClose }: { onClose: () => void }) {
  const { me, branchId } = useAuth()
  const qc = useQueryClient()
  const courses = useQuery({ queryKey: ['courses'], queryFn: () => api.get<Course[]>('/courses') })
  const fields = useFields('lead')
  const [f, setF] = useState({ full_name: '', contact_name: '', phone: '', course_id: '', source: 'Instagram', age_or_grade: '', preferred_time: '', comment: '', branch_id: branchId || me?.branches?.[0]?.id || '' })
  const [cf, setCf] = useState<Record<string, unknown>>({})
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [dups, setDups] = useState<{ kind: string; full_name: string }[] | null>(null)
  const m = useMutation({
    mutationFn: () => api.post<{ id: string; duplicates: { kind: string; full_name: string }[] }>('/leads', { ...f, custom_fields: cf }),
    onSuccess: (r) => { qc.invalidateQueries({ queryKey: ['leads'] }); qc.invalidateQueries({ queryKey: ['leads-new-count'] }); if (r.duplicates.length) setDups(r.duplicates); else onClose() },
    onError: (e) => setErrors(errorsByField(e)),
  })
  return (
    <Modal open wide title={tr('Новый лид')} onClose={onClose} footer={dups ? <Button variant="primary" onClick={onClose}>{tr('Понятно')}</Button> : <><Button onClick={onClose}>{tr('Отмена')}</Button><Button variant="primary" loading={m.isPending} onClick={() => m.mutate()}>{tr('Создать')}</Button></>}>
      {dups ? <Banner tone="yellow">{tr('Лид создан, но с этим телефоном уже есть:')} {dups.map((d) => `${d.full_name} (${d.kind === 'student' ? tr('ученик') : tr('лид')})`).join(', ')}{tr('. Проверьте, не дубликат ли это.')}</Banner> : (
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={tr('Имя ученика')} error={errors.full_name}><Input autoFocus value={f.full_name} onChange={(e) => setF({ ...f, full_name: e.target.value })} /></Field>
          <Field label={tr('Контактное лицо')}><Input value={f.contact_name} onChange={(e) => setF({ ...f, contact_name: e.target.value })} placeholder={tr('Мама — Гульнара')} /></Field>
          <Field label={tr('Телефон')} error={errors.phone}><Input value={f.phone} onChange={(e) => setF({ ...f, phone: e.target.value })} placeholder="+7 7__ ___-__-__" /></Field>
          <Field label={tr('Источник')}><Select value={f.source} onChange={(e) => setF({ ...f, source: e.target.value })}>{sources.map((s) => <option key={s}>{s}</option>)}</Select></Field>
          <Field label={tr('Интересует курс')}><Select value={f.course_id} onChange={(e) => setF({ ...f, course_id: e.target.value })}><option value="">—</option>{courses.data?.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}</Select></Field>
          <Field label={tr('Возраст / класс')}><Input value={f.age_or_grade} onChange={(e) => setF({ ...f, age_or_grade: e.target.value })} /></Field>
          <Field label={tr('Удобное время')}><Input value={f.preferred_time} onChange={(e) => setF({ ...f, preferred_time: e.target.value })} placeholder={tr('будни после 15:00')} /></Field>
          <Field label={tr('Филиал')}><Select value={f.branch_id} onChange={(e) => setF({ ...f, branch_id: e.target.value })}>{me?.branches?.map((b) => <option key={b.id} value={b.id}>{b.name}</option>)}</Select></Field>
          {fields.data?.data.map((x) => <CFInput key={x.key} field={x} value={cf[x.key]} onChange={(v) => setCf({ ...cf, [x.key]: v })} error={errors['custom_fields.' + x.key]} />)}
          <Field label={tr('Комментарий')} className="sm:col-span-2"><Textarea value={f.comment} onChange={(e) => setF({ ...f, comment: e.target.value })} /></Field>
        </div>
      )}
    </Modal>
  )
}
