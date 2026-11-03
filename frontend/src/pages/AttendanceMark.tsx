import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronLeft } from 'lucide-react'
import { ApiError, api } from '../lib/api'
import { dayMonth, time } from '../lib/format'
import { Avatar, Badge, Spinner, cx } from '../components/ui'

type Item = { student_id: string | null; lead_id: string | null; full_name: string; is_trial: boolean; status: string | null; reason: string | null; info: Record<string, { label: string; value: string }>; note?: string }
type Data = { lesson: { id: string; group_name: string; status: string; classroom: string | null; topic: string | null; starts_at: string; ends_at: string; closed_at: string | null }; roster: Item[]; editable: boolean; requires_reason: boolean }

const statuses = [
  { v: 'present', l: 'Был', on: 'border-green-400 bg-green-50 text-green-800' },
  { v: 'late', l: 'Опоздал', on: 'border-amber-400 bg-amber-50 text-amber-800' },
  { v: 'absent', l: 'Не был', on: 'border-red-400 bg-red-50 text-red-800' },
  { v: 'excused', l: 'Уваж.', on: 'border-indigo-400 bg-indigo-50 text-indigo-800' },
]

// Отметка посещаемости с телефона (кабинет преподавателя / Telegram Mini App): минимум действий, зоны касания ≥ 44 px.
export default function AttendanceMark() {
  const { lessonId } = useParams()
  const nav = useNavigate()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['attendance', lessonId], queryFn: () => api.get<Data>(`/lessons/${lessonId}/attendance`) })
  const [marks, setMarks] = useState<Record<string, string>>({})
  const [topic, setTopic] = useState('')
  const [reason, setReason] = useState('')
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)
  useEffect(() => {
    if (!q.data) return
    const m: Record<string, string> = {}
    q.data.roster.forEach((r) => { if (r.status) m[key(r)] = r.status })
    setMarks(m)
    setTopic(q.data.lesson.topic ?? '')
  }, [q.data])
  const save = useMutation({
    mutationFn: () => api.put(`/lessons/${lessonId}/attendance`, {
      items: q.data!.roster.filter((r) => marks[key(r)]).map((r) => ({ student_id: r.student_id ?? '', lead_id: r.lead_id ?? '', status: r.is_trial && marks[key(r)] === 'present' ? 'trial' : marks[key(r)] })),
      topic, close: true, change_reason: reason,
    }),
    onSuccess: () => { setSaved(true); qc.invalidateQueries(); setTimeout(() => nav(-1), 900) },
    onError: (e) => setError(e instanceof ApiError ? e.message : 'Не удалось сохранить'),
  })
  if (q.isLoading) return <Spinner className="h-full" />
  if (!q.data) return <div className="p-10 text-center text-muted">Занятие не найдено</div>
  const { lesson, roster, editable } = q.data
  const total = roster.length
  const done = roster.filter((r) => marks[key(r)]).length
  const left = total - done
  const notStarted = new Date(lesson.starts_at).getTime() > Date.now() + 30 * 60e3

  return (
    <div className="mx-auto flex h-full max-w-xl flex-col bg-page">
      <header className="border-b border-line bg-white px-4 pb-4 pt-5">
        <div className="flex items-start gap-3">
          <button onClick={() => nav(-1)} className="flex size-11 items-center justify-center rounded-full hover:bg-gray-100" aria-label="Назад"><ChevronLeft className="size-6" /></button>
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-xl font-bold">{lesson.group_name}</h1>
            <div className="text-sm text-muted first-letter:uppercase">{dayMonth(new Date(lesson.starts_at))} {time(lesson.starts_at)}–{time(lesson.ends_at)}{lesson.classroom && ` · ${lesson.classroom}`}</div>
          </div>
          {lesson.status === 'done' && <Badge tone="green">Проведено</Badge>}
        </div>
        <div className="mt-4 flex items-center gap-3">
          <div className="h-2 flex-1 overflow-hidden rounded-full bg-gray-200"><div className="h-full rounded-full bg-brand-600 transition-all" style={{ width: `${total ? (done / total) * 100 : 0}%` }} /></div>
          <span className="text-sm font-semibold tabular">{done} из {total}</span>
        </div>
        {editable && left > 0 && (
          <button onClick={() => { const m = { ...marks }; roster.forEach((r) => { if (!m[key(r)]) m[key(r)] = 'present' }); setMarks(m) }}
            className="mt-3 h-12 w-full rounded-xl border border-brand-200 bg-brand-50 text-[15px] font-semibold text-brand-700 active:bg-brand-100">
            Отметить остальных «Был»
          </button>
        )}
      </header>

      <div className="flex-1 space-y-3 overflow-y-auto p-4">
        {notStarted && <div className="rounded-xl bg-amber-50 p-4 text-sm text-amber-900">Отметить посещаемость можно после начала занятия.</div>}
        {!editable && <div className="rounded-xl bg-gray-100 p-4 text-sm text-gray-700">Срок редактирования истёк. Изменить отметки может администратор с правом «Изменение закрытой посещаемости».</div>}
        {roster.length === 0 && <div className="py-10 text-center text-sm text-muted">В группе нет учеников на эту дату</div>}
        {roster.map((r) => (
          <div key={key(r)} className="rounded-2xl border border-line bg-white p-4">
            <div className="flex items-center gap-3">
              <Avatar name={r.full_name} tone="gray" />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 truncate text-[15px] font-semibold">{r.full_name}{r.is_trial && <Badge tone="purple">Пробный</Badge>}</div>
                <div className="truncate text-[13px] text-muted">
                  {[...Object.values(r.info ?? {}).sort((a, b) => order(a.label) - order(b.label)).map((i) => `${shortLabel(i.label)} ${i.value}`), r.note].filter(Boolean).join(' · ') || ' '}
                </div>
              </div>
            </div>
            <div className="mt-3 grid grid-cols-4 gap-2">
              {statuses.map((s) => (
                <button key={s.v} disabled={!editable || notStarted} onClick={() => setMarks({ ...marks, [key(r)]: s.v })}
                  className={cx('h-12 rounded-xl border text-sm font-semibold transition-colors disabled:opacity-60', marks[key(r)] === s.v || (s.v === 'present' && marks[key(r)] === 'trial') ? s.on : 'border-line text-gray-700 active:bg-gray-50')}>
                  {s.l}
                </button>
              ))}
            </div>
          </div>
        ))}
      </div>

      <footer className="space-y-3 border-t border-line bg-white p-4">
        <input value={topic} onChange={(e) => setTopic(e.target.value)} placeholder="Тема урока (необязательно)" disabled={!editable}
          className="h-12 w-full rounded-xl border border-line px-4 text-[15px] outline-none focus:border-brand-500" />
        {q.data.requires_reason && editable && (
          <input value={reason} onChange={(e) => setReason(e.target.value)} placeholder="Причина изменения закрытого занятия (обязательно)"
            className="h-12 w-full rounded-xl border border-amber-300 px-4 text-[15px] outline-none" />
        )}
        {error && <div className="text-sm text-red-700">{error}</div>}
        <button disabled={!editable || notStarted || left > 0 || save.isPending} onClick={() => { setError(''); save.mutate() }}
          className={cx('h-14 w-full rounded-xl text-[17px] font-semibold text-white transition-colors', saved ? 'bg-green-600' : left > 0 ? 'bg-gray-500' : 'bg-brand-600 active:bg-brand-700')}>
          {saved ? 'Сохранено ✓' : left > 0 ? `Сохранить · осталось ${left}` : save.isPending ? 'Сохранение…' : lesson.status === 'done' ? 'Сохранить изменения' : 'Сохранить и закрыть занятие'}
        </button>
      </footer>
    </div>
  )
}

const key = (r: { student_id: string | null; lead_id: string | null }) => (r.student_id ?? '') + '|' + (r.lead_id ?? '')
const order = (l: string) => ({ 'Целевой балл ЕНТ': 0, 'Профильные предметы': 1 } as Record<string, number>)[l] ?? 2
const shortLabel = (l: string) => ({ 'Целевой балл ЕНТ': 'цель', 'Профильные предметы': 'профиль:', 'Класс': 'класс' } as Record<string, string>)[l] ?? l.toLowerCase() + ':'
