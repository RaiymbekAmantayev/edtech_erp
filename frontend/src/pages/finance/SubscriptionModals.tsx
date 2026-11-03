import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { api, errorsByField } from '../../lib/api'
import { addDays, isoDay, money, parseMoney } from '../../lib/format'
import type { Course } from '../../lib/types'
import { Button, Field, Input, Modal, Select } from '../../components/ui'

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
    <Modal open title="Абонемент" onClose={onClose} footer={<><Button onClick={onClose}>Отмена</Button><Button variant="primary" loading={m.isPending} onClick={() => m.mutate()}>Оформить</Button></>}>
      <div className="space-y-4">
        <Field label="Курс"><Select value={f.course_id} onChange={(e) => setF({ ...f, course_id: e.target.value, lessons_total: '', price: '' })}><option value="">Любой курс</option>{courses.data?.map((c) => <option key={c.id} value={c.id}>{c.name} · {money(c.price_tiyn)}</option>)}</Select></Field>
        <Field label="Тип" error={errors.type}>
          <Select value={f.type} onChange={(e) => setF({ ...f, type: e.target.value })}>
            <option value="lessons">По количеству занятий</option><option value="monthly">Календарный месяц</option><option value="period">За период</option><option value="unlimited">Безлимит</option>
          </Select>
        </Field>
        <div className="grid grid-cols-2 gap-4">
          {f.type === 'lessons' && <Field label="Занятий" error={errors.lessons_total}><Input type="number" value={lessons} onChange={(e) => setF({ ...f, lessons_total: e.target.value })} /></Field>}
          <Field label="Стоимость, ₸" error={errors.price_tiyn}><Input value={price} onChange={(e) => setF({ ...f, price: e.target.value })} /></Field>
          <Field label="Начало"><Input type="date" value={f.starts_on} onChange={(e) => setF({ ...f, starts_on: e.target.value })} /></Field>
          <Field label="Окончание" hint={f.type === 'lessons' ? 'по умолчанию +45 дней' : f.type === 'monthly' ? 'по умолчанию — месяц' : ''} error={errors.ends_on}>
            <Input type="date" value={f.ends_on} onChange={(e) => setF({ ...f, ends_on: e.target.value })} />
          </Field>
        </div>
        <p className="text-xs text-muted">Стоимость начисляется на баланс сразу; оплата закрывает задолженность. Занятия списываются при закрытии занятия по правилам посещаемости.</p>
      </div>
    </Modal>
  )
}

export function FreezeModal({ subscriptionId, onClose }: { subscriptionId: string; onClose: () => void }) {
  const [f, setF] = useState({ starts_on: isoDay(), ends_on: addDays(isoDay(), 13), reason: 'Болезнь' })
  const [error, setError] = useState('')
  const m = useMutation({ mutationFn: () => api.post(`/subscriptions/${subscriptionId}/freeze`, f), onSuccess: onClose, onError: (e) => setError((e as Error).message) })
  const days = Math.round((new Date(f.ends_on).getTime() - new Date(f.starts_on).getTime()) / 864e5) + 1
  return (
    <Modal open title="Заморозка абонемента" onClose={onClose} footer={<><Button onClick={onClose}>Отмена</Button><Button variant="primary" loading={m.isPending} onClick={() => m.mutate()}>Заморозить на {days > 0 ? days : 0} дн.</Button></>}>
      <div className="space-y-4">
        <div className="grid grid-cols-2 gap-4">
          <Field label="С"><Input type="date" value={f.starts_on} onChange={(e) => setF({ ...f, starts_on: e.target.value })} /></Field>
          <Field label="По"><Input type="date" value={f.ends_on} onChange={(e) => setF({ ...f, ends_on: e.target.value })} /></Field>
        </div>
        <Field label="Причина"><Select value={f.reason} onChange={(e) => setF({ ...f, reason: e.target.value })}><option>Болезнь</option><option>Отпуск</option><option>Другое</option></Select></Field>
        <p className="text-xs text-muted">Во время заморозки занятия не списываются, а срок абонемента продлевается на длительность заморозки. Разморозка — автоматически.</p>
        {error && <div className="text-sm text-red-700">{error}</div>}
      </div>
    </Modal>
  )
}
