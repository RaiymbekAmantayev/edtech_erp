import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { BookOpen, ChevronLeft } from 'lucide-react'
import { ApiError, api } from '../lib/api'
import { dayMonth, time } from '../lib/format'
import { autoGrade, gradeStyle, num, summarize, type TestGrade, type TestType } from '../lib/tests'
import { Avatar, Badge, Spinner, cx } from '../components/ui'

type Item = { student_id: string | null; lead_id: string | null; full_name: string; is_trial: boolean; status: string | null; reason: string | null; info: Record<string, { label: string; value: string }>; note?: string }
type KtpTopic = { id: string; number: number; title: string; homework: string | null; done?: boolean }
type LessonTest = TestType & { required: boolean }
type Entry = { student_id: string; test_type_id: string; scores: { skill_id: string; score: number | null; grade_id: string | null }[] }
type Data = {
  lesson: { id: string; group_name: string; status: string; classroom: string | null; topic: string | null; homework: string | null; ktp_topic_id: string | null; starts_at: string; ends_at: string; closed_at: string | null }
  roster: Item[]
  editable: boolean
  requires_reason: boolean
  ktp: { categories: { id: string; name: string; topics: KtpTopic[] }[]; suggested_topic_id: string | null }
  tests: LessonTest[]
  grades: TestGrade[]
  results: Entry[]
}
// ученик → тест → навык → значение (балл или id оценки)
type Scores = Record<string, Record<string, Record<string, string>>>

const statuses = [
  { v: 'present', l: 'Был', on: 'border-green-400 bg-green-50 text-green-800' },
  { v: 'late', l: 'Опоздал', on: 'border-amber-400 bg-amber-50 text-amber-800' },
  { v: 'absent', l: 'Не был', on: 'border-red-400 bg-red-50 text-red-800' },
  { v: 'excused', l: 'Уваж.', on: 'border-indigo-400 bg-indigo-50 text-indigo-800' },
]
const came = (s?: string) => s === 'present' || s === 'late' || s === 'trial'
const field = 'w-full rounded-xl border border-line bg-white px-3 text-[15px] outline-none focus:border-brand-500 disabled:bg-gray-50'

// Отметка урока с телефона (кабинет преподавателя / Telegram Mini App): посещаемость, тема и ДЗ из КТП, баллы за тесты.
export default function AttendanceMark() {
  const { lessonId } = useParams()
  const nav = useNavigate()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['attendance', lessonId], queryFn: () => api.get<Data>(`/lessons/${lessonId}/attendance`) })
  const [marks, setMarks] = useState<Record<string, string>>({})
  const [topic, setTopic] = useState('')
  const [homework, setHomework] = useState('')
  const [ktpId, setKtpId] = useState('')
  const [cat, setCat] = useState('')
  const [scores, setScores] = useState<Scores>({})
  const [reason, setReason] = useState('')
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)
  useEffect(() => {
    if (!q.data) return
    const d = q.data
    const m: Record<string, string> = {}
    d.roster.forEach((r) => { if (r.status) m[key(r)] = r.status })
    setMarks(m)
    const sc: Scores = {}
    for (const e of d.results) for (const s of e.scores) {
      ((sc[e.student_id] ??= {})[e.test_type_id] ??= {})[s.skill_id] = s.grade_id ?? (s.score != null ? String(s.score) : '')
    }
    setScores(sc)
    // новая тема по плану подставляется сама: следующая непройденная тема КТП с её ДЗ
    const all = d.ktp.categories.flatMap((c) => c.topics.map((t) => ({ ...t, cat: c.id })))
    const pick = d.lesson.ktp_topic_id ?? (!d.lesson.topic && d.lesson.status !== 'done' ? d.ktp.suggested_topic_id : null)
    const t = all.find((x) => x.id === pick)
    setKtpId(t?.id ?? '')
    setCat(t?.cat ?? d.ktp.categories[0]?.id ?? '')
    setTopic(d.lesson.topic ?? (t && !d.lesson.ktp_topic_id ? t.title : ''))
    setHomework(d.lesson.homework ?? (t && !d.lesson.ktp_topic_id ? t.homework ?? '' : ''))
  }, [q.data])

  const presentIds = (q.data?.roster ?? []).filter((r) => r.student_id && came(marks[key(r)])).map((r) => r.student_id!)
  const missing = (q.data?.tests ?? []).filter((t) => t.required).reduce((n, t) => n + presentIds.filter((sid) => !Object.values(scores[sid]?.[t.id] ?? {}).some((v) => v.trim())).length, 0)
  const over = (q.data?.tests ?? []).some((t) => t.scoring === 'score' && presentIds.some((sid) => t.skills.some((k) => Number((scores[sid]?.[t.id]?.[k.skill_id] ?? '').replace(',', '.')) > k.max_score)))

  const save = useMutation({
    mutationFn: () => {
      const d = q.data!
      const tests: Entry[] = []
      for (const sid of presentIds) for (const t of d.tests) {
        const v = scores[sid]?.[t.id] ?? {}
        if (!Object.values(v).some((x) => x.trim())) continue
        tests.push({ student_id: sid, test_type_id: t.id, scores: t.skills.map((k) => {
          const x = (v[k.skill_id] ?? '').trim()
          return t.scoring === 'score' ? { skill_id: k.skill_id, score: x ? Number(x.replace(',', '.')) : null, grade_id: null } : { skill_id: k.skill_id, score: null, grade_id: x || null }
        }) })
      }
      return api.put(`/lessons/${lessonId}/attendance`, {
        items: d.roster.filter((r) => marks[key(r)]).map((r) => ({ student_id: r.student_id ?? '', lead_id: r.lead_id ?? '', status: r.is_trial && marks[key(r)] === 'present' ? 'trial' : marks[key(r)] })),
        topic, homework, ktp_topic_id: ktpId, tests, close: true, change_reason: reason,
      })
    },
    onSuccess: () => { setSaved(true); qc.invalidateQueries(); setTimeout(() => nav(-1), 900) },
    onError: (e) => setError(e instanceof ApiError ? e.message : 'Не удалось сохранить'),
  })
  if (q.isLoading) return <Spinner className="h-full" />
  if (!q.data) return <div className="p-10 text-center text-muted">Занятие не найдено</div>
  const { lesson, roster, editable, ktp, tests, grades } = q.data
  const total = roster.length
  const done = roster.filter((r) => marks[key(r)]).length
  const left = total - done
  const notStarted = new Date(lesson.starts_at).getTime() > Date.now() + 30 * 60e3
  const disabled = !editable || notStarted
  const topics = ktp.categories.find((c) => c.id === cat)?.topics ?? []
  const chooseTopic = (id: string) => {
    setKtpId(id)
    const t = topics.find((x) => x.id === id)
    if (t) { setTopic(t.title); setHomework(t.homework ?? '') }
  }
  const setScore = (sid: string, tid: string, kid: string, v: string) =>
    setScores({ ...scores, [sid]: { ...scores[sid], [tid]: { ...scores[sid]?.[tid], [kid]: v } } })
  const blocked = left > 0 ? `осталось ${left}` : missing > 0 && lesson.status !== 'done' ? `обязательные тесты · ${missing}` : over ? 'балл больше максимума' : ''

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
        {notStarted && <div className="rounded-xl bg-amber-50 p-4 text-sm text-amber-900">Отметить урок можно после начала занятия.</div>}
        {!editable && <div className="rounded-xl bg-gray-100 p-4 text-sm text-gray-700">Срок редактирования истёк. Изменить отметки может администратор с правом «Изменение закрытой посещаемости».</div>}

        <section className="space-y-3 rounded-2xl border border-line bg-white p-4">
          <div className="flex items-center gap-2 text-[15px] font-semibold"><BookOpen className="size-4 text-brand-600" />Тема и домашнее задание</div>
          {ktp.categories.length > 0 && (
            <>
              {ktp.categories.length > 1 && (
                <div className="-mx-1 flex gap-2 overflow-x-auto px-1">
                  {ktp.categories.map((c) => (
                    <button key={c.id} disabled={disabled} onClick={() => setCat(c.id)}
                      className={cx('h-9 shrink-0 rounded-full border px-3 text-[13px] font-medium', c.id === cat ? 'border-brand-600 bg-brand-600 text-white' : 'border-line')}>{c.name}</button>
                  ))}
                </div>
              )}
              <label className="block">
                <span className="mb-1 block text-[12px] text-muted">Тема по КТП</span>
                <select value={topics.some((t) => t.id === ktpId) ? ktpId : ''} disabled={disabled} onChange={(e) => chooseTopic(e.target.value)} className={cx(field, 'h-12')}>
                  <option value="">— вне плана —</option>
                  {topics.map((t) => <option key={t.id} value={t.id}>{t.done && t.id !== lesson.ktp_topic_id ? '✓ ' : ''}{t.number}. {t.title}{t.id === ktp.suggested_topic_id ? ' — по плану' : ''}</option>)}
                </select>
              </label>
            </>
          )}
          <label className="block">
            <span className="mb-1 block text-[12px] text-muted">Тема урока</span>
            <input value={topic} onChange={(e) => setTopic(e.target.value)} disabled={disabled} placeholder="Необязательно" className={cx(field, 'h-12')} />
          </label>
          <label className="block">
            <span className="mb-1 block text-[12px] text-muted">Домашнее задание — придёт родителям</span>
            <textarea value={homework} onChange={(e) => setHomework(e.target.value)} disabled={disabled} rows={3} placeholder="Без ДЗ" className={cx(field, 'py-2.5')} />
          </label>
        </section>

        {tests.length > 0 && (
          <div className="px-1 text-[13px] text-muted">
            Тесты на уроке: {tests.map((t) => t.name + (t.required ? ' (обязательный)' : '')).join(', ')}. Баллы вводятся у пришедших учеников.
          </div>
        )}
        {roster.length === 0 && <div className="py-10 text-center text-sm text-muted">В группе нет учеников на эту дату</div>}
        {roster.map((r) => {
          const st = marks[key(r)]
          const showTests = !!r.student_id && came(st) && tests.length > 0
          return (
            <div key={key(r)} className="rounded-2xl border border-line bg-white p-4">
              <div className="flex items-center gap-3">
                <Avatar name={r.full_name} tone="gray" />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2 truncate text-[15px] font-semibold">{r.full_name}{r.is_trial && <Badge tone="purple">Пробный</Badge>}</div>
                  <div className="truncate text-[13px] text-muted">
                    {[...Object.values(r.info ?? {}).sort((a, b) => order(a.label) - order(b.label)).map((i) => `${shortLabel(i.label)} ${i.value}`), r.note].filter(Boolean).join(' · ') || ' '}
                  </div>
                </div>
              </div>
              <div className="mt-3 grid grid-cols-4 gap-2">
                {statuses.map((s) => (
                  <button key={s.v} disabled={disabled} onClick={() => setMarks({ ...marks, [key(r)]: s.v })}
                    className={cx('h-12 rounded-xl border text-sm font-semibold transition-colors disabled:opacity-60', st === s.v || (s.v === 'present' && st === 'trial') ? s.on : 'border-line text-gray-700 active:bg-gray-50')}>
                    {s.l}
                  </button>
                ))}
              </div>
              {showTests && tests.map((t) => (
                <TestInputs key={t.id} t={t} grades={grades} values={scores[r.student_id!]?.[t.id] ?? {}} disabled={disabled}
                  onChange={(kid, v) => setScore(r.student_id!, t.id, kid, v)} />
              ))}
            </div>
          )
        })}
      </div>

      <footer className="space-y-3 border-t border-line bg-white p-4">
        {q.data.requires_reason && editable && (
          <input value={reason} onChange={(e) => setReason(e.target.value)} placeholder="Причина изменения закрытого занятия (обязательно)"
            className="h-12 w-full rounded-xl border border-amber-300 px-4 text-[15px] outline-none" />
        )}
        {error && <div className="text-sm text-red-700">{error}</div>}
        <button disabled={disabled || !!blocked || save.isPending} onClick={() => { setError(''); save.mutate() }}
          className={cx('h-14 w-full rounded-xl text-[17px] font-semibold text-white transition-colors', saved ? 'bg-green-600' : blocked ? 'bg-gray-500' : 'bg-brand-600 active:bg-brand-700')}>
          {saved ? 'Сохранено ✓' : blocked ? `Сохранить · ${blocked}` : save.isPending ? 'Сохранение…' : lesson.status === 'done' ? 'Сохранить изменения' : 'Сохранить и закрыть занятие'}
        </button>
      </footer>
    </div>
  )
}

function TestInputs({ t, grades, values, disabled, onChange }: { t: LessonTest; grades: TestGrade[]; values: Record<string, string>; disabled: boolean; onChange: (skillId: string, v: string) => void }) {
  const sum = t.scoring === 'score' ? summarize(t, values) : null
  const auto = sum ? autoGrade(grades, sum.percent) : null
  const empty = !Object.values(values).some((v) => v.trim())
  return (
    <div className={cx('mt-3 rounded-xl border p-3', t.required && empty ? 'border-red-200 bg-red-50/50' : 'border-line bg-gray-50/60')}>
      <div className="mb-2 flex items-center justify-between gap-2">
        <div className="truncate text-[13px] font-semibold">{t.name}{t.required && <span className="text-red-600"> *</span>}</div>
        {sum && sum.filled > 0 && (
          <div className="flex items-center gap-1.5 text-[13px]">
            <b className="tabular">{num(sum.total)}/{num(sum.max)}</b>
            {auto && <span className="rounded-full bg-gray-100 px-2 py-0.5 text-[11px] font-semibold" style={gradeStyle(auto.color)}>{auto.name}</span>}
          </div>
        )}
      </div>
      <div className={cx('grid gap-2', t.skills.length >= 3 ? 'grid-cols-3' : t.skills.length === 2 ? 'grid-cols-2' : 'grid-cols-1')}>
        {t.skills.map((k) => {
          const v = values[k.skill_id] ?? ''
          return (
            <label key={k.skill_id} className="block min-w-0">
              <span className="mb-1 block truncate text-[11px] text-muted">{k.name}{t.scoring === 'score' && ` / ${num(k.max_score)}`}</span>
              {t.scoring === 'score' ? (
                <input inputMode="decimal" value={v} disabled={disabled} onChange={(e) => onChange(k.skill_id, e.target.value.replace(/[^\d.,]/g, ''))}
                  className={cx(field, 'h-11 text-center tabular', Number(v.replace(',', '.')) > k.max_score && 'border-red-400 bg-red-50')} />
              ) : (
                <select value={v} disabled={disabled} onChange={(e) => onChange(k.skill_id, e.target.value)} className={cx(field, 'h-11 px-2')}>
                  <option value="">—</option>
                  {grades.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
                </select>
              )}
            </label>
          )
        })}
      </div>
    </div>
  )
}

const key = (r: { student_id: string | null; lead_id: string | null }) => (r.student_id ?? '') + '|' + (r.lead_id ?? '')
const order = (l: string) => ({ 'Целевой балл ЕНТ': 0, 'Профильные предметы': 1 } as Record<string, number>)[l] ?? 2
const shortLabel = (l: string) => ({ 'Целевой балл ЕНТ': 'цель', 'Профильные предметы': 'профиль:', 'Класс': 'класс' } as Record<string, string>)[l] ?? l.toLowerCase() + ':'
