import { useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, CalendarDays, ChevronLeft, ChevronRight, Plus } from 'lucide-react'
import { ApiError, api, errorsByField } from '../lib/api'
import { useAuth } from '../lib/auth'
import { addDays, dayMonth, dayStart, isoDay, time, weekdayShort } from '../lib/format'
import type { Classroom, Employee, Group, Lesson } from '../lib/types'
import { Badge, Banner, Button, Drawer, Field, Input, Modal, Segmented, Select, Spinner, StatusBadge, cx, lessonState } from '../components/ui'

const START_H = 8, END_H = 21, PX = 64 // пикселей на час

function tone(l: Lesson) {
  if (l.conflict && l.status !== 'cancelled') return 'border-red-300 bg-red-50 text-red-900'
  if (l.status === 'done') return 'border-gray-300 bg-gray-100 text-gray-700'
  if (l.status === 'cancelled') return 'border-gray-200 bg-white text-gray-400 line-through'
  if (l.status === 'needs_decision') return 'border-amber-300 bg-amber-50 text-amber-900'
  if (l.trials > 0) return 'border-purple-300 bg-purple-50 text-purple-900'
  if (l.group_type === 'individual') return 'border-emerald-300 bg-emerald-50 text-emerald-900'
  return 'border-indigo-200 bg-indigo-50 text-indigo-950'
}
const minutesOf = (iso: string) => { const [h, m] = time(iso).split(':').map(Number); return h * 60 + m }

export default function Schedule() {
  const { me, branchId, can, isTeacherOnly } = useAuth()
  const qc = useQueryClient()
  const [params, setParams] = useSearchParams()
  const [day, setDay] = useState(isoDay())
  const [mode, setMode] = useState<'rooms' | 'teachers' | 'week'>(isTeacherOnly ? 'week' : 'rooms')
  const [branch, setBranch] = useState(branchId || me?.branches?.[0]?.id || '')
  const [open, setOpen] = useState<Lesson | null>(null)
  const [create, setCreate] = useState(params.get('new') === '1')
  const weekStart = addDays(day, -((new Date(day + 'T12:00:00+05:00').getUTCDay() + 6) % 7))
  const from = mode === 'week' ? weekStart : day
  const to = mode === 'week' ? addDays(weekStart, 7) : addDays(day, 1)
  const lessons = useQuery({
    queryKey: ['lessons', from, to, isTeacherOnly ? '' : branch],
    queryFn: () => api.get<Lesson[]>(`/lessons?from=${dayStart(from).toISOString()}&to=${dayStart(to).toISOString()}${isTeacherOnly ? '' : '&branch_id=' + branch}`),
    refetchInterval: 60_000,
  })
  const rooms = useQuery({ queryKey: ['classrooms', branch], queryFn: () => api.get<Classroom[]>(`/classrooms?branch_id=${branch}`), enabled: !isTeacherOnly })
  const teachers = useQuery({ queryKey: ['employees', 'teachers'], queryFn: () => api.get<Employee[]>('/employees?teachers=1'), enabled: !isTeacherOnly })
  const list = lessons.data ?? []
  const conflicts = list.filter((l) => l.conflict && l.status !== 'cancelled')
  const decisions = useQuery({ queryKey: ['lessons-decisions', branch], queryFn: () => api.get<Lesson[]>(`/lessons?from=${new Date().toISOString()}&branch_id=${branch}&limit=2000`).then((r) => r.filter((l) => l.status === 'needs_decision')), enabled: can('schedule.view') })
  const refresh = () => { qc.invalidateQueries({ queryKey: ['lessons'] }); qc.invalidateQueries({ queryKey: ['lessons-decisions'] }) }

  const columns = mode === 'rooms'
    ? (rooms.data ?? []).map((r) => ({ id: r.id, title: r.name, sub: r.is_online ? 'без лимита' : `${r.capacity ?? '—'} мест`, match: (l: Lesson) => l.classroom_id === r.id }))
    : (teachers.data ?? []).filter((t) => !branch || !t.branch_ids.length || t.branch_ids.includes(branch)).map((t) => ({ id: t.id, title: t.full_name, sub: t.subjects.slice(0, 2).join(', '), match: (l: Lesson) => l.teacher_id === t.id }))
  const isToday = day === isoDay()
  const [now, setNow] = useState(new Date())
  useEffect(() => { const t = setInterval(() => setNow(new Date()), 60_000); return () => clearInterval(t) }, [])
  const nowTop = ((minutesOf(now.toISOString()) - START_H * 60) / 60) * PX
  const firstConflict = conflicts[0]
  const conflictPair = firstConflict && list.filter((l) => l.id !== firstConflict.id && l.teacher_id === firstConflict.teacher_id && l.starts_at < firstConflict.ends_at && l.ends_at > firstConflict.starts_at)[0]

  return (
    <div className="p-4 lg:p-8">
      <div className="flex flex-wrap items-center gap-3">
        <h1 className="mr-2 text-[26px] font-bold">Расписание</h1>
        <div className="flex items-center gap-1">
          <Button size="sm" className="!px-2" onClick={() => setDay(addDays(day, mode === 'week' ? -7 : -1))} aria-label="Назад"><ChevronLeft className="size-4" /></Button>
          <label className="relative">
            <Button size="sm" className="min-w-36 first-letter:uppercase">{mode === 'week' ? `${dayMonth(new Date(weekStart + 'T12:00:00+05:00'))} – ${dayMonth(new Date(addDays(weekStart, 6) + 'T12:00:00+05:00'))}` : dayMonth(new Date(day + 'T12:00:00+05:00'))}</Button>
            <input type="date" value={day} onChange={(e) => e.target.value && setDay(e.target.value)} className="absolute inset-0 cursor-pointer opacity-0" />
          </label>
          <Button size="sm" className="!px-2" onClick={() => setDay(addDays(day, mode === 'week' ? 7 : 1))} aria-label="Вперёд"><ChevronRight className="size-4" /></Button>
        </div>
        <button onClick={() => setDay(isoDay())} className="text-sm font-medium text-brand-600 hover:underline">Сегодня</button>
        {!isTeacherOnly && <Segmented value={mode} onChange={setMode} items={[{ value: 'rooms', label: 'Кабинеты' }, { value: 'teachers', label: 'Преподаватели' }, { value: 'week', label: 'Неделя' }]} />}
        <div className="flex-1" />
        {!isTeacherOnly && (me?.branches?.length ?? 0) > 1 && (
          <Select className="!h-9 w-48" value={branch} onChange={(e) => setBranch(e.target.value)}>{me?.branches?.map((b) => <option key={b.id} value={b.id}>{b.name}</option>)}</Select>
        )}
        {can('schedule.edit') && <Button variant="primary" size="sm" onClick={() => setCreate(true)}><Plus className="size-4" />Занятие</Button>}
      </div>

      {(firstConflict || (decisions.data?.length ?? 0) > 0) && (
        <div className="mt-4 grid gap-3 lg:grid-cols-2">
          {firstConflict && (
            <Banner tone="red" icon={<AlertTriangle className="size-4" />} action={<button className="font-semibold" onClick={() => setOpen(firstConflict)}>Решить</button>}>
              <b>Конфликт в {time(firstConflict.starts_at)}:</b> {firstConflict.teacher_name} поставлен в «{firstConflict.group_name}»{conflictPair && <> и «{conflictPair.group_name}» ({conflictPair.classroom_name})</>} одновременно.
            </Banner>
          )}
          {(decisions.data?.length ?? 0) > 0 && (
            <Banner tone="blue" icon={<CalendarDays className="size-4" />} action={<button className="font-semibold" onClick={() => { setDay(isoDay(new Date(decisions.data![0].starts_at))); setMode('rooms') }}>Открыть</button>}>
              Нерабочие дни: {decisions.data!.length} занятий серий ждут решения о переносе (ближайшее — {dayMonth(new Date(decisions.data![0].starts_at))}).
            </Banner>
          )}
        </div>
      )}

      <div className="mt-4 overflow-hidden rounded-xl border border-line bg-white">
        {lessons.isLoading ? <Spinner /> : mode === 'week' ? (
          <div className="grid grid-cols-7 divide-x divide-line overflow-x-auto" style={{ minWidth: 900 }}>
            {Array.from({ length: 7 }, (_, i) => addDays(weekStart, i)).map((d, i) => (
              <div key={d} className={cx('min-h-96', d === isoDay() && 'bg-brand-50/30')}>
                <div className="border-b border-line px-3 py-2.5 text-sm font-semibold">{weekdayShort[i]}, {d.slice(8)}.{d.slice(5, 7)}</div>
                <div className="space-y-1.5 p-2">
                  {list.filter((l) => isoDay(new Date(l.starts_at)) === d).map((l) => (
                    <button key={l.id} onClick={() => setOpen(l)} className={cx('w-full rounded-lg border px-2 py-1.5 text-left text-xs', tone(l))}>
                      <div className="font-semibold">{time(l.starts_at)} {l.group_name}</div>
                      <div className="opacity-80">{l.classroom_name}{!isTeacherOnly && ` · ${l.teacher_name ?? ''}`}</div>
                    </button>
                  ))}
                </div>
              </div>
            ))}
          </div>
        ) : (
          <div className="overflow-x-auto">
            <div className="flex" style={{ minWidth: 120 + columns.length * 170 }}>
              <div className="w-14 shrink-0 border-r border-line">
                <div className="h-14 border-b border-line" />
                {Array.from({ length: END_H - START_H }, (_, i) => <div key={i} className="border-b border-line/60 pr-2 pt-1 text-right text-xs text-muted tabular" style={{ height: PX }}>{String(START_H + i).padStart(2, '0')}:00</div>)}
              </div>
              {columns.map((c) => (
                <div key={c.id} className="min-w-[170px] flex-1 border-r border-line last:border-r-0">
                  <div className="h-14 border-b border-line px-3 py-2"><div className="truncate text-sm font-semibold">{c.title}</div><div className="truncate text-xs text-muted">{c.sub}</div></div>
                  <div className="relative" style={{ height: (END_H - START_H) * PX }}>
                    {Array.from({ length: END_H - START_H }, (_, i) => <div key={i} className="border-b border-line/60" style={{ height: PX }} />)}
                    {list.filter(c.match).map((l) => {
                      const top = ((minutesOf(l.starts_at) - START_H * 60) / 60) * PX
                      const h = Math.max(28, ((minutesOf(l.ends_at) - minutesOf(l.starts_at)) / 60) * PX - 4)
                      return (
                        <button key={l.id} onClick={() => setOpen(l)} className={cx('absolute inset-x-1.5 overflow-hidden rounded-lg border px-2 py-1.5 text-left text-xs', tone(l))} style={{ top: top + 2, height: h }}>
                          <div className="truncate font-semibold">{l.group_name}</div>
                          <div className="truncate opacity-80 tabular">{time(l.starts_at)}–{time(l.ends_at)} · {l.students}{l.capacity ? `/${l.capacity}` : ''}{l.trials > 0 && ` · ${l.trials} пробный`}</div>
                          {h > 50 && <div className="truncate opacity-80">{mode === 'rooms' ? l.teacher_name : l.classroom_name}</div>}
                          {h > 66 && (l.conflict || l.state !== 'planned') && <div className="truncate font-semibold">{l.conflict ? 'Конфликт' : lessonState[l.state]?.[0]}</div>}
                        </button>
                      )
                    })}
                    {isToday && nowTop > 0 && nowTop < (END_H - START_H) * PX && <div className="pointer-events-none absolute inset-x-0 border-t-2 border-red-500" style={{ top: nowTop }} />}
                  </div>
                </div>
              ))}
              {columns.length === 0 && <div className="p-10 text-sm text-muted">Добавьте кабинеты в «Настройки → Филиалы и кабинеты»</div>}
            </div>
          </div>
        )}
      </div>
      <div className="mt-3 flex flex-wrap gap-4 text-xs text-muted">
        {[['border-indigo-200 bg-indigo-50', 'Групповое'], ['border-emerald-300 bg-emerald-50', 'Индивидуальное'], ['border-purple-300 bg-purple-50', 'С пробным учеником'], ['border-red-300 bg-red-50', 'Конфликт'], ['border-gray-300 bg-gray-100', 'Проведено'], ['border-amber-300 bg-amber-50', 'Нерабочий день']].map(([c, l]) => (
          <span key={l} className="flex items-center gap-1.5"><span className={cx('size-3 rounded border', c)} />{l}</span>
        ))}
      </div>

      {open && <LessonDrawer lesson={open} onClose={() => setOpen(null)} onChanged={() => { refresh(); setOpen(null) }} />}
      {create && <LessonModal day={day} branch={branch} onClose={() => { setCreate(false); if (params.get('new')) setParams({}) }} onSaved={() => { refresh(); setCreate(false) }} />}
    </div>
  )
}

function LessonDrawer({ lesson: l, onClose, onChanged }: { lesson: Lesson; onClose: () => void; onChanged: () => void }) {
  const { can } = useAuth()
  const [move, setMove] = useState(false)
  const [trial, setTrial] = useState(false)
  const cancel = useMutation({ mutationFn: (scope: string) => api.post(`/lessons/${l.id}/cancel`, { scope, reason: '' }), onSuccess: onChanged })
  const keep = useMutation({ mutationFn: () => api.patch(`/lessons/${l.id}`, { scope: 'this', status: 'planned', force: true }), onSuccess: onChanged })
  return (
    <Drawer open title={l.group_name} onClose={onClose}>
      <div className="space-y-4 text-sm">
        <div className="flex flex-wrap gap-2"><StatusBadge map={lessonState} value={l.state} />{l.conflict && <Badge tone="red">Конфликт</Badge>}{l.group_type === 'individual' && <Badge tone="green">Индивидуальное</Badge>}{l.schedule_id && <Badge>Серия</Badge>}</div>
        <dl className="grid grid-cols-2 gap-3">
          <div><dt className="text-muted">Время</dt><dd className="font-medium tabular">{dayMonth(new Date(l.starts_at))}, {time(l.starts_at)}–{time(l.ends_at)}</dd></div>
          <div><dt className="text-muted">Курс</dt><dd className="font-medium">{l.course_name}</dd></div>
          <div><dt className="text-muted">Преподаватель</dt><dd className="font-medium">{l.teacher_name ?? '—'}</dd></div>
          <div><dt className="text-muted">Кабинет</dt><dd className="font-medium">{l.classroom_name ?? '—'}</dd></div>
          <div><dt className="text-muted">Учеников</dt><dd className="font-medium">{l.students}{l.capacity ? ` из ${l.capacity}` : ''}{l.trials ? ` + ${l.trials} пробных` : ''}</dd></div>
          <div><dt className="text-muted">Отмечено</dt><dd className="font-medium">{l.marked}</dd></div>
          {l.topic && <div className="col-span-2"><dt className="text-muted">Тема</dt><dd>{l.topic}</dd></div>}
        </dl>
        {l.status === 'needs_decision' && <Banner tone="yellow">Занятие попало на нерабочий день. Перенесите его или оставьте в расписании.</Banner>}
        <div className="flex flex-wrap gap-2 border-t border-line pt-4">
          {l.status !== 'cancelled' && new Date(l.starts_at).getTime() < Date.now() + 30 * 60e3 && <Link to={`/attendance/${l.id}`}><Button variant="primary">{l.status === 'done' ? 'Посещаемость' : 'Отметить посещаемость'}</Button></Link>}
          {can('schedule.edit') && l.status !== 'done' && l.status !== 'cancelled' && <Button onClick={() => setMove(true)}>Перенести / заменить</Button>}
          {can('schedule.edit') && l.status === 'needs_decision' && <Button loading={keep.isPending} onClick={() => keep.mutate()}>Провести в этот день</Button>}
          {(can('schedule.edit') || can('leads.edit')) && l.status !== 'done' && l.status !== 'cancelled' && <Button onClick={() => setTrial(true)}>+ Пробный ученик</Button>}
          {can('schedule.edit') && l.status !== 'done' && l.status !== 'cancelled' && (
            <>
              <Button variant="danger" loading={cancel.isPending} onClick={() => confirm('Отменить только это занятие? Родители получат уведомление.') && cancel.mutate('this')}>Отменить</Button>
              {l.schedule_id && <Button variant="danger" onClick={() => confirm('Отменить это и все последующие занятия серии?') && cancel.mutate('following')}>Отменить серию далее</Button>}
            </>
          )}
        </div>
      </div>
      {move && <LessonModal lesson={l} onClose={() => setMove(false)} onSaved={onChanged} />}
      {trial && <TrialModal lesson={l} onClose={() => setTrial(false)} onSaved={onChanged} />}
    </Drawer>
  )
}

function LessonModal({ lesson, day, branch, onClose, onSaved }: { lesson?: Lesson; day?: string; branch?: string; onClose: () => void; onSaved: () => void }) {
  const groups = useQuery({ queryKey: ['groups', 'all-types'], queryFn: () => api.get<Group[]>('/groups?type=all') })
  const teachers = useQuery({ queryKey: ['employees', 'teachers'], queryFn: () => api.get<Employee[]>('/employees?teachers=1') })
  const rooms = useQuery({ queryKey: ['classrooms', 'all'], queryFn: () => api.get<Classroom[]>('/classrooms') })
  const [f, setF] = useState({
    group_id: lesson?.group_id ?? '', date: lesson ? isoDay(new Date(lesson.starts_at)) : day ?? isoDay(), start_time: lesson ? time(lesson.starts_at) : '10:00',
    end_time: lesson ? time(lesson.ends_at) : '11:30', teacher_id: lesson?.teacher_id ?? '', classroom_id: lesson?.classroom_id ?? '', type: 'regular', topic: '', scope: 'this',
  })
  const [conflicts, setConflicts] = useState<{ kind: string; message: string }[]>([])
  const [errors, setErrors] = useState<Record<string, string>>({})
  const group = groups.data?.find((g) => g.id === f.group_id)
  const branchId = group?.branch_id ?? branch
  useEffect(() => {
    if (!lesson && group) setF((x) => ({ ...x, teacher_id: x.teacher_id || group.teacher_id || '', classroom_id: x.classroom_id || group.classroom_id || '' }))
  }, [group, lesson])
  const checkQ = useQuery({
    queryKey: ['check', f, lesson?.id],
    queryFn: () => api.post<{ kind: string; message: string }[]>(`/lessons/check${lesson ? '?exclude=' + lesson.id : ''}`, { ...f, teacher_id: f.teacher_id || null, classroom_id: f.classroom_id || null }),
    enabled: !!f.group_id && /^\d\d:\d\d$/.test(f.start_time) && /^\d\d:\d\d$/.test(f.end_time),
  })
  const body = (force: boolean) => ({ ...f, teacher_id: f.teacher_id || '', classroom_id: f.classroom_id || '', force })
  const m = useMutation({
    mutationFn: (force: boolean) => lesson ? api.patch(`/lessons/${lesson.id}`, body(force)) : api.post('/lessons', body(force)),
    onSuccess: onSaved,
    onError: (e) => {
      if (e instanceof ApiError && e.code === 'SCHEDULE_CONFLICT') setConflicts((e.meta?.conflicts as never) ?? [])
      else setErrors(errorsByField(e))
    },
  })
  const live = checkQ.data ?? []
  return (
    <Modal open title={lesson ? 'Перенос / замена' : 'Новое занятие'} onClose={onClose}
      footer={<><Button onClick={onClose}>Отмена</Button>
        {conflicts.length > 0 && <Button variant="danger" loading={m.isPending} onClick={() => m.mutate(true)}>Сохранить с конфликтом</Button>}
        <Button variant="primary" loading={m.isPending} onClick={() => m.mutate(false)}>Сохранить</Button></>}>
      <div className="space-y-4">
        {!lesson && (
          <Field label="Группа или индивидуальное" error={errors.group_id}>
            <Select value={f.group_id} onChange={(e) => setF({ ...f, group_id: e.target.value, teacher_id: '', classroom_id: '' })}>
              <option value="">Выберите</option>
              {groups.data?.filter((g) => !branch || g.branch_id === branch).map((g) => <option key={g.id} value={g.id}>{g.name}{g.type === 'individual' ? ' (индив.)' : ''}</option>)}
            </Select>
          </Field>
        )}
        <div className="grid grid-cols-3 gap-3">
          <Field label="Дата"><Input type="date" value={f.date} onChange={(e) => setF({ ...f, date: e.target.value })} /></Field>
          <Field label="Начало"><Input type="time" value={f.start_time} onChange={(e) => setF({ ...f, start_time: e.target.value })} /></Field>
          <Field label="Конец" error={errors.end_time}><Input type="time" value={f.end_time} onChange={(e) => setF({ ...f, end_time: e.target.value })} /></Field>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Преподаватель"><Select value={f.teacher_id} onChange={(e) => setF({ ...f, teacher_id: e.target.value })}><option value="">—</option>{teachers.data?.map((t) => <option key={t.id} value={t.id}>{t.full_name}</option>)}</Select></Field>
          <Field label="Кабинет"><Select value={f.classroom_id} onChange={(e) => setF({ ...f, classroom_id: e.target.value })}><option value="">—</option>{rooms.data?.filter((r) => !branchId || r.branch_id === branchId).map((r) => <option key={r.id} value={r.id}>{r.name}{r.capacity ? ` (${r.capacity})` : ''}</option>)}</Select></Field>
        </div>
        {!lesson && <Field label="Тип"><Select value={f.type} onChange={(e) => setF({ ...f, type: e.target.value })}><option value="regular">Обычное</option><option value="makeup">Отработка</option><option value="trial">Пробное</option><option value="online">Онлайн</option></Select></Field>}
        {lesson?.schedule_id && (
          <Field label="Применить изменение">
            <Select value={f.scope} onChange={(e) => setF({ ...f, scope: e.target.value })}>
              <option value="this">Только это занятие</option><option value="following">Это и все последующие</option><option value="all">Вся серия (с сегодняшнего дня)</option>
            </Select>
          </Field>
        )}
        {(conflicts.length ? conflicts : live).map((c, i) => <Banner key={i} tone={c.kind === 'capacity' ? 'yellow' : 'red'}>{c.message}</Banner>)}
      </div>
    </Modal>
  )
}

function TrialModal({ lesson, onClose, onSaved }: { lesson: Lesson; onClose: () => void; onSaved: () => void }) {
  const leads = useQuery({ queryKey: ['leads', 'for-trial'], queryFn: () => api.get<{ id: string; full_name: string; stage: string; phone: string }[]>('/leads?stage=new,contacted,trial_scheduled') })
  const [lead, setLead] = useState('')
  const m = useMutation({ mutationFn: () => api.post(`/lessons/${lesson.id}/guests`, { lead_id: lead }), onSuccess: onSaved })
  const list = useMemo(() => leads.data ?? [], [leads.data])
  return (
    <Modal open title="Записать на пробное занятие" onClose={onClose} footer={<><Button onClick={onClose}>Отмена</Button><Button variant="primary" disabled={!lead} loading={m.isPending} onClick={() => m.mutate()}>Записать</Button></>}>
      <Select value={lead} onChange={(e) => setLead(e.target.value)}><option value="">Выберите лида</option>{list.map((l) => <option key={l.id} value={l.id}>{l.full_name}</option>)}</Select>
      <p className="mt-2 text-xs text-muted">Пробный ученик появится в посещаемости преподавателя с пометкой «Пробный», лид перейдёт на этап «Записан на пробное». Пробное занятие с абонемента не списывается.</p>
    </Modal>
  )
}
