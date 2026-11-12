import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { api, errorsByField } from '../../lib/api'
import { addDays, isoDay, money, parseMoney } from '../../lib/format'
import type { Course } from '../../lib/types'
import { Button, Field, Input, Modal, Select } from '../../components/ui'
import { tr } from '../../lib/i18n'

export function SubscriptionModal({ studentId, courseId, onClose }: { studentId: string; courseId?: string; onClose: () => void }) {
  const courses = useQuery({ queryKey: ['courses'], queryFn: () => api.get<Course[]>('/courses') })
  const [f, setF] = useState({ course_id: courseId ?? '', type: 'lessons', lessons_total: '', price: '', starts_on: isoDay(), ends_on: '' })
  const [errors, setErrors] = useState<Record<string, string>>({})
  const course = courses.data?.find((c) => c.id === f.course_id)
  const lessons = f.lessons_total || (course ? String(course.lessons_count) : '')
  const price = f.price || (course ? String(course.price_tiyn / 100) : '')
  const m = useMutation({
    mutationFn: () => api.post('/subscriptions', {
      student_id: studentId, course_id: f.course_id || undefined, type: f.type, price_tiyn: parseMoney(price) ?? 0,
      lessons_total: f.type === 'lessons' ? Number(lessons) : undefined, starts_on: f.starts_on, ends_on: f.ends_on || undefined,
    }),
    onSuccess: onClose,
    onError: (e) => setErrors(errorsByField(e)),
  })
  return (
    <Modal open title={tr('Абонемент')} onClose={onClose} footer={<><Button onClick={onClose}>{tr('Отмена')}</Button><Button variant="primary" loading={m.isPending} onClick={() => m.mutate()}>{tr('Оформить')}</Button></>}>
      <div className="space-y-4">
        <Field label={tr('Курс')}><Select value={f.course_id} onChange={(e) => setF({ ...f, course_id: e.target.value, lessons_total: '', price: '' })}><option value="">{tr('Любой курс')}</option>{courses.data?.map((c) => <option key={c.id} value={c.id}>{c.name} · {money(c.price_tiyn)}</option>)}</Select></Field>
        <Field label={tr('Тип')} error={errors.type}>
          <Select value={f.type} onChange={(e) => setF({ ...f, type: e.target.value })}>
            <option value="lessons">{tr('По количеству занятий')}</option><option value="monthly">{tr('Календарный месяц')}</option><option value="period">{tr('За период')}</option><option value="unlimited">{tr('Безлимит')}</option>
          </Select>
        </Field>
        <div className="grid grid-cols-2 gap-4">
          {f.type === 'lessons' && <Field label={tr('Занятий')} error={errors.lessons_total}><Input type="number" value={lessons} onChange={(e) => setF({ ...f, lessons_total: e.target.value })} /></Field>}
          <Field label={tr('Стоимость, ₸')} error={errors.price_tiyn}><Input value={price} onChange={(e) => setF({ ...f, price: e.target.value })} /></Field>
          <Field label={tr('Начало')}><Input type="date" value={f.starts_on} onChange={(e) => setF({ ...f, starts_on: e.target.value })} /></Field>
          <Field label={tr('Окончание')} hint={f.type === 'lessons' ? tr('по умолчанию +45 дней') : f.type === 'monthly' ? tr('по умолчанию — месяц') : ''} error={errors.ends_on}>
            <Input type="date" value={f.ends_on} onChange={(e) => setF({ ...f, ends_on: e.target.value })} />
          </Field>
        </div>
        <p className="text-xs text-muted">{tr('Стоимость начисляется на баланс сразу; оплата закрывает задолженность. Занятия списываются при закрытии занятия по правилам посещаемости.')}</p>
      </div>
    </Modal>
  )
}

export function FreezeModal({ subscriptionId, onClose }: { subscriptionId: string; onClose: () => void }) {
  const [f, setF] = useState({ starts_on: isoDay(), ends_on: addDays(isoDay(), 13), reason: tr('Болезнь') })
  const [error, setError] = useState('')
  const m = useMutation({ mutationFn: () => api.post(`/subscriptions/${subscriptionId}/freeze`, f), onSuccess: onClose, onError: (e) => setError((e as Error).message) })
  const days = Math.round((new Date(f.ends_on).getTime() - new Date(f.starts_on).getTime()) / 864e5) + 1
  return (
    <Modal open title={tr('Заморозка абонемента')} onClose={onClose} footer={<><Button onClick={onClose}>{tr('Отмена')}</Button><Button variant="primary" loading={m.isPending} onClick={() => m.mutate()}>{tr('Заморозить на')} {days > 0 ? days : 0} {tr('дн.')}</Button></>}>
      <div className="space-y-4">
        <div className="grid grid-cols-2 gap-4">
          <Field label={tr('С')}><Input type="date" value={f.starts_on} onChange={(e) => setF({ ...f, starts_on: e.target.value })} /></Field>
          <Field label={tr('По')}><Input type="date" value={f.ends_on} onChange={(e) => setF({ ...f, ends_on: e.target.value })} /></Field>
        </div>
        <Field label={tr('Причина')}><Select value={f.reason} onChange={(e) => setF({ ...f, reason: e.target.value })}><option>{tr('Болезнь')}</option><option>{tr('Отпуск')}</option><option>{tr('Другое')}</option></Select></Field>
        <p className="text-xs text-muted">{tr('Во время заморозки занятия не списываются, а срок абонемента продлевается на длительность заморозки. Разморозка — автоматически.')}</p>
        {error && <div className="text-sm text-red-700">{error}</div>}
      </div>
    </Modal>
  )
}
