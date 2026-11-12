import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowDown, ArrowUp } from 'lucide-react'
import { api, errorsByField } from '../../lib/api'
import { CoursePicker, courseNames, useCourses } from '../../components/CoursePicker'
import { Badge, Button, Card, Checkbox, Empty, Field, Input, Modal, Spinner, Textarea, Toast, cx } from '../../components/ui'
import { tr } from '../../lib/i18n'

type Topic = { id: string; number: number; title: string; homework: string | null; hours: number | null }
type Category = { id: string; name: string; course_ids: string[]; archived: boolean; topics: Topic[] }

// КТП школы: категории (предмет, уровень, класс — как удобно) и темы уроков с домашними заданиями.
export default function Ktp() {
  const qc = useQueryClient()
  const [archived, setArchived] = useState(false)
  const q = useQuery({ queryKey: ['ktp', archived], queryFn: () => api.get<Category[]>('/ktp' + (archived ? '?archived=1' : '')) })
  const courses = useCourses()
  const [sel, setSel] = useState('')
  const [catModal, setCatModal] = useState<Category | 'new' | null>(null)
  const [toast, setToast] = useState('')
  const refresh = () => qc.invalidateQueries({ queryKey: ['ktp'] })
  if (!q.data) return <Spinner />
  const cats = q.data
  const cat = cats.find((c) => c.id === sel) ?? cats[0]
  return (
    <div className="max-w-6xl space-y-5">
      <div>
        <h1 className="text-[22px] font-bold">{tr('КТП')}</h1>
        <p className="mt-1 text-sm text-muted">{tr('Календарно-тематический план. Разбейте его на категории так, как удобно школе: по предметам, уровням или классам. Привяжите категорию к курсам — при отметке занятия преподаватель выберет тему из списка, и ДЗ подставится само.')}</p>
      </div>
      <div className="grid gap-5 lg:grid-cols-[280px_minmax(0,1fr)]">
        <Card title={tr('Категории')} bodyClass="!px-0 !pb-2" action={<Button size="sm" variant="primary" onClick={() => setCatModal('new')}>{tr('+ Категория')}</Button>}>
          {cats.length === 0 ? <div className="px-5 pb-3 text-sm text-muted">{tr('Создайте первую категорию, например «English A2» или «ЕНТ Математика 11 класс».')}</div> : cats.map((c) => (
            <button key={c.id} onClick={() => setSel(c.id)} className={cx('block w-full px-5 py-2.5 text-left', c.id === cat?.id ? 'bg-brand-50' : 'hover:bg-gray-50', c.archived && 'opacity-60')}>
              <div className={cx('text-sm', c.id === cat?.id && 'font-semibold text-brand-700')}>{c.name}</div>
              <div className="text-xs text-muted">{c.topics.length} {tr('тем ·')} {courseNames(c.course_ids, courses.data)}</div>
            </button>
          ))}
          <div className="border-t border-line px-5 pt-2"><Checkbox checked={archived} onChange={setArchived} label={tr('Показать архив')} /></div>
        </Card>
        {cat ? <Topics key={cat.id} cat={cat} courses={courseNames(cat.course_ids, courses.data)} onEdit={() => setCatModal(cat)} onChanged={refresh} onToast={setToast} />
          : <Card><Empty title={tr('Категорий пока нет')} text={tr('КТП появится при отметке занятий, как только вы добавите категорию и темы.')} /></Card>}
      </div>
      {catModal && <CategoryModal cat={catModal === 'new' ? null : catModal} onClose={() => setCatModal(null)} onSaved={(id) => { setCatModal(null); if (id) setSel(id); refresh(); setToast(tr('Сохранено')) }} />}
      {toast && <Toast text={toast} onDone={() => setToast('')} />}
    </div>
  )
}

function CategoryModal({ cat, onClose, onSaved }: { cat: Category | null; onClose: () => void; onSaved: (id?: string) => void }) {
  const [name, setName] = useState(cat?.name ?? '')
  const [courses, setCourses] = useState<string[]>(cat?.course_ids ?? [])
  const save = useMutation({
    mutationFn: () => cat ? api.patch(`/ktp/categories/${cat.id}`, { name, course_ids: courses }) : api.post<{ id: string }>('/ktp/categories', { name, course_ids: courses }),
    onSuccess: (r) => onSaved((r as { id?: string } | undefined)?.id),
  })
  const archive = useMutation({ mutationFn: () => api.patch(`/ktp/categories/${cat!.id}`, { archived: !cat!.archived }), onSuccess: () => onSaved() })
  return (
    <Modal open onClose={onClose} title={cat ? tr('Категория КТП') : tr('Новая категория КТП')} wide
      footer={<>
        {cat && <Button variant="danger" className="mr-auto" onClick={() => archive.mutate()}>{cat.archived ? tr('Восстановить') : tr('В архив')}</Button>}
        <Button onClick={onClose}>{tr('Отмена')}</Button>
        <Button variant="primary" loading={save.isPending} disabled={!name.trim()} onClick={() => save.mutate()}>{tr('Сохранить')}</Button>
      </>}>
      <div className="space-y-4">
        <Field label={tr('Название')} required><Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder={tr('English A2, ЕНТ Математика 11 класс…')} maxLength={160} /></Field>
        <Field label={tr('Курсы')} hint={tr('На занятиях этих курсов преподаватель увидит темы категории. «Все курсы» — категория предлагается, если к курсу группы ничего не привязано.')}>
          <CoursePicker value={courses} onChange={setCourses} />
        </Field>
        {save.error && <div className="text-sm text-red-700">{(save.error as Error).message}</div>}
      </div>
    </Modal>
  )
}

function Topics({ cat, courses, onEdit, onChanged, onToast }: { cat: Category; courses: string; onEdit: () => void; onChanged: () => void; onToast: (s: string) => void }) {
  const [edit, setEdit] = useState<Topic | 'new' | null>(null)
  const [bulk, setBulk] = useState(false)
  const del = useMutation({ mutationFn: (id: string) => api.del(`/ktp/topics/${id}`), onSuccess: onChanged })
  const order = useMutation({ mutationFn: (ids: string[]) => api.put(`/ktp/categories/${cat.id}/order`, { ids }), onSuccess: onChanged })
  const move = (i: number, d: number) => {
    const ids = cat.topics.map((t) => t.id)
    const [x] = ids.splice(i, 1)
    ids.splice(i + d, 0, x)
    order.mutate(ids)
  }
  return (
    <Card bodyClass="!px-0 !pb-0" title={<span className="flex items-center gap-2">{cat.name}{cat.archived && <Badge>{tr('Архив')}</Badge>}</span>}
      action={<div className="flex gap-2"><Button size="sm" variant="ghost" onClick={onEdit}>{tr('Настроить')}</Button><Button size="sm" onClick={() => setBulk(true)}>{tr('Вставить списком')}</Button><Button size="sm" variant="primary" onClick={() => setEdit('new')}>{tr('+ Тема')}</Button></div>}>
      <div className="px-5 pb-3 text-[13px] text-muted">{tr('Курсы:')} {courses} · {cat.topics.length} {tr('тем')}</div>
      {cat.topics.length === 0 ? <Empty title={tr('Тем пока нет')} text={tr('Добавьте темы по одной или вставьте весь план из таблицы кнопкой «Вставить списком».')} /> : (
        <div className="overflow-x-auto border-t border-line">
          <table className="w-full text-sm">
            <thead><tr className="border-b border-line text-left text-[11px] font-semibold uppercase tracking-wide text-muted">
              <th className="w-12 px-4 py-2.5">№</th><th className="px-4 py-2.5">{tr('Тема урока')}</th><th className="px-4 py-2.5">{tr('Домашнее задание')}</th><th className="w-16 px-4 py-2.5">{tr('Часы')}</th><th className="w-36" />
            </tr></thead>
            <tbody className="divide-y divide-line">
              {cat.topics.map((t, i) => (
                <tr key={t.id} className="align-top">
                  <td className="px-4 py-2.5 tabular text-muted">{t.number}</td>
                  <td className="px-4 py-2.5 font-medium">{t.title}</td>
                  <td className="whitespace-pre-line px-4 py-2.5 text-gray-700">{t.homework ?? <span className="text-muted">—</span>}</td>
                  <td className="px-4 py-2.5 tabular">{t.hours ?? ''}</td>
                  <td className="px-2 py-1.5 text-right whitespace-nowrap">
                    <button disabled={i === 0} onClick={() => move(i, -1)} className="rounded p-1.5 text-muted hover:bg-gray-100 disabled:opacity-30" aria-label={tr('Выше')}><ArrowUp className="size-4" /></button>
                    <button disabled={i === cat.topics.length - 1} onClick={() => move(i, 1)} className="rounded p-1.5 text-muted hover:bg-gray-100 disabled:opacity-30" aria-label={tr('Ниже')}><ArrowDown className="size-4" /></button>
                    <Button size="sm" variant="ghost" onClick={() => setEdit(t)}>{tr('Изменить')}</Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {edit && <TopicModal catId={cat.id} topic={edit === 'new' ? null : edit} onClose={() => setEdit(null)}
        onDelete={edit !== 'new' ? () => { if (confirm(tr('Удалить тему? В проведённых занятиях тема и ДЗ сохранятся.'))) { del.mutate(edit.id); setEdit(null) } } : undefined}
        onSaved={() => { setEdit(null); onChanged(); onToast(tr('Тема сохранена')) }} />}
      {bulk && <BulkModal catId={cat.id} onClose={() => setBulk(false)} onSaved={(n) => { setBulk(false); onChanged(); onToast(tr('Добавлено тем: {0}', n)) }} />}
    </Card>
  )
}

function TopicModal({ catId, topic, onClose, onSaved, onDelete }: { catId: string; topic: Topic | null; onClose: () => void; onSaved: () => void; onDelete?: () => void }) {
  const [title, setTitle] = useState(topic?.title ?? '')
  const [homework, setHomework] = useState(topic?.homework ?? '')
  const [hours, setHours] = useState(topic?.hours != null ? String(topic.hours) : '')
  const save = useMutation({
    mutationFn: () => {
      const h = hours.trim() ? Number(hours.replace(',', '.')) : null
      const body = { title, homework, hours: h, clear_hours: h == null }
      return topic ? api.patch(`/ktp/topics/${topic.id}`, body) : api.post(`/ktp/categories/${catId}/topics`, body)
    },
    onSuccess: onSaved,
  })
  const err = errorsByField(save.error)
  return (
    <Modal open onClose={onClose} title={topic ? tr('Тема №{0}', topic.number) : tr('Новая тема')} wide
      footer={<>{onDelete && <Button variant="danger" className="mr-auto" onClick={onDelete}>{tr('Удалить')}</Button>}<Button onClick={onClose}>{tr('Отмена')}</Button><Button variant="primary" loading={save.isPending} disabled={!title.trim()} onClick={() => save.mutate()}>{tr('Сохранить')}</Button></>}>
      <div className="space-y-4">
        <Field label={tr('Тема урока')} required error={err.title}><Input autoFocus value={title} onChange={(e) => setTitle(e.target.value)} maxLength={500} placeholder={tr('Present Simple: утвердительные предложения')} /></Field>
        <Field label={tr('Домашнее задание')} error={err.homework} hint={tr('Подставится при выборе темы на уроке — преподаватель сможет его изменить.')}><Textarea rows={4} value={homework} onChange={(e) => setHomework(e.target.value)} placeholder={tr('Учебник с. 24, упр. 3–5')} /></Field>
        <Field label={tr('Часы')} error={err.hours}><Input className="!w-24" inputMode="decimal" value={hours} onChange={(e) => setHours(e.target.value)} placeholder="1" /></Field>
      </div>
    </Modal>
  )
}

// Вставка КТП списком: строка — тема; ДЗ и часы через табуляцию (копирование из Excel) или «|».
function BulkModal({ catId, onClose, onSaved }: { catId: string; onClose: () => void; onSaved: (n: number) => void }) {
  const [text, setText] = useState('')
  const items = text.split('\n').map((l) => l.trim()).filter(Boolean).map((l) => {
    const [title, homework, hours] = l.split(/\t|\s*\|\s*/)
    const h = hours ? Number(hours.replace(',', '.')) : NaN
    return { title: title.replace(/^\d+[.)]\s*/, ''), homework: homework ?? '', hours: Number.isNaN(h) ? null : h }
  }).filter((i) => i.title)
  const save = useMutation({ mutationFn: () => api.post(`/ktp/categories/${catId}/topics`, { items }), onSuccess: () => onSaved(items.length) })
  return (
    <Modal open onClose={onClose} title={tr('Вставить темы списком')} wide
      footer={<><Button onClick={onClose}>{tr('Отмена')}</Button><Button variant="primary" loading={save.isPending} disabled={!items.length} onClick={() => save.mutate()}>{tr('Добавить')} {items.length || ''}</Button></>}>
      <p className="mb-3 text-sm text-muted">{tr('Каждая строка — тема урока. Скопируйте столбцы «Тема», «ДЗ», «Часы» из Excel или разделите их знаком «|». Номера в начале строки уберутся.')}</p>
      <Textarea rows={12} className="font-mono text-[13px]" value={text} onChange={(e) => setText(e.target.value)}
        placeholder={tr('1. Знакомство. Алфавит | Выучить алфавит | 1\n2. Числа 1–20 | Упр. 2, с. 10 | 1\n3. Present Simple | Упр. 1–3, с. 14')} />
      {items.length > 0 && <div className="mt-2 text-sm text-muted">{tr('Будет добавлено тем:')} <b className="text-ink">{items.length}</b>{tr('; с ДЗ:')} {items.filter((i) => i.homework).length}</div>}
      {save.error && <div className="mt-2 text-sm text-red-700">{(save.error as Error).message}</div>}
    </Modal>
  )
}
