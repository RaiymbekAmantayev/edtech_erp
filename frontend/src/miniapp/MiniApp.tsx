import { useEffect, useState, type ReactNode } from 'react'
import { Route, Routes, useNavigate } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { BarChart3, BookOpen, CalendarDays, CircleUser, ClipboardList, GraduationCap, History, NotebookPen, Wallet, X } from 'lucide-react'
import { ApiError, api, setToken } from '../lib/api'
import { date, dateShort, dayMonth, money, plural, time } from '../lib/format'
import { Avatar, Badge, Spinner, attendanceStatus, cx, type Tone } from '../components/ui'
import AttendanceMark from '../pages/AttendanceMark'
import { gradeStyle, num, type TestResult } from '../lib/tests'
import { hours, payTypes, rateText, type Payroll } from '../lib/payroll'
import { hasPortalToken, loadTelegram, portal, setPortalToken, type Lesson, type Overview, type PortalMe, type TeacherCabinet } from './portal'

type Session = { kind: 'student' | 'parent' | 'staff' }

// Личный кабинет (Telegram Mini App): /app?t=<id школы>. Открывается кнопкой «Кабинет» в боте школы или по ссылке в браузере.
export default function MiniApp() {
  const [tenant] = useState(() => {
    const t = new URLSearchParams(location.search).get('t') ?? sessionStorage.getItem('emp.app.t') ?? ''
    if (t) sessionStorage.setItem('emp.app.t', t)
    return t
  })
  const [initData, setInitData] = useState<string | null>(null)
  const [session, setSession] = useState<Session | null>(null)
  const [state, setState] = useState<'loading' | 'login' | 'ready' | 'error'>('loading')
  const [error, setError] = useState('')
  const school = useQuery({ queryKey: ['app-school', tenant], queryFn: () => portal<{ name: string; bot_username?: string }>('GET', '/miniapp/school?t=' + tenant), enabled: !!tenant, retry: false })

  useEffect(() => {
    if (!tenant) { setState('error'); setError('Ссылка на кабинет неполная. Откройте кабинет кнопкой «Кабинет» в боте учебного центра.'); return }
    let alive = true
    ;(async () => {
      const tg = await loadTelegram()
      if (!alive) return
      setInitData(tg?.initData ?? '')
      if (tg) {
        try {
          const r = await portal<{ linked?: boolean; kind?: string; access_token?: string; portal_token?: string }>('POST', '/miniapp/auth', { tenant_id: tenant, init_data: tg.initData })
          if (r.access_token) { setToken(r.access_token); setSession({ kind: 'staff' }); setState('ready'); return }
          if (r.portal_token) { setPortalToken(r.portal_token); setSession({ kind: r.kind as Session['kind'] }); setState('ready'); return }
        } catch (e) {
          if (e instanceof ApiError && e.status >= 500) { setError(e.message); setState('error'); return }
        }
      } else if (hasPortalToken()) {
        // вход в обычном браузере: токен вкладки ещё действителен
        try {
          const me = await portal<PortalMe>('GET', '/portal/me')
          setSession({ kind: me.kind }); setState('ready'); return
        } catch { setPortalToken(null) }
      }
      setState('login')
    })()
    return () => { alive = false }
  }, [tenant])

  const logout = () => { setPortalToken(null); setSession(null); setState('login') }

  if (state === 'loading' || (tenant && school.isLoading)) return <Spinner className="h-full" />
  if (state === 'error' || school.isError) return <Center><div className="text-[15px]">{error || (school.error as Error)?.message || 'Учебный центр не найден'}</div></Center>
  if (state === 'login' || !session) return <Login tenant={tenant} school={school.data!.name} initData={initData ?? ''} onDone={(k) => { setSession({ kind: k }); setState('ready') }} />
  return (
    <Routes>
      <Route path="lesson/:lessonId" element={<AttendanceMark />} />
      <Route path="*" element={session.kind === 'staff' ? <TeacherHome /> : <PortalHome onLogout={logout} />} />
    </Routes>
  )
}

const Center = ({ children }: { children: ReactNode }) => <div className="flex h-full items-center justify-center p-8 text-center text-muted">{children}</div>

// ---------------------------------------------------------------- вход по ИИН

function Login({ tenant, school, initData, onDone }: { tenant: string; school: string; initData: string; onDone: (k: 'student' | 'parent') => void }) {
  const [kind, setKind] = useState<'student' | 'parent'>('student')
  const [iin, setIin] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const m = useMutation({
    mutationFn: () => portal<{ portal_token: string; kind: 'student' | 'parent' }>('POST', '/portal/login', { tenant_id: tenant, kind, iin: iin.replace(/\D/g, ''), password, init_data: initData }),
    onSuccess: (r) => { setPortalToken(r.portal_token); onDone(r.kind) },
    onError: (e) => setError(e instanceof ApiError && e.fields.length ? 'Проверьте ИИН: 12 цифр' : (e as Error).message),
  })
  return (
    <div className="mx-auto flex min-h-full max-w-md flex-col bg-white px-5 pb-8 pt-10">
      <div className="text-[13px] font-semibold uppercase tracking-wide text-brand-600">Личный кабинет</div>
      <h1 className="mt-1 text-2xl font-bold">{school}</h1>
      <div className="mt-6 grid grid-cols-2 gap-1 rounded-xl bg-gray-100 p-1">
        {([['student', 'Я ученик'], ['parent', 'Я родитель']] as const).map(([k, l]) => (
          <button key={k} onClick={() => setKind(k)} className={cx('h-11 rounded-lg text-[15px] font-semibold', kind === k ? 'bg-white shadow-sm' : 'text-muted')}>{l}</button>
        ))}
      </div>
      <form className="mt-6 space-y-4" onSubmit={(e) => { e.preventDefault(); setError(''); m.mutate() }}>
        <label className="block">
          <span className="text-sm font-medium">{kind === 'student' ? 'ИИН ученика' : 'Ваш ИИН'}</span>
          <input inputMode="numeric" autoComplete="username" maxLength={12} value={iin} onChange={(e) => setIin(e.target.value.replace(/\D/g, ''))} placeholder="12 цифр"
            className="mt-1.5 h-12 w-full rounded-xl border border-line px-4 text-[17px] tracking-wider outline-none focus:border-brand-500" />
        </label>
        <label className="block">
          <span className="text-sm font-medium">Пароль</span>
          <input type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)}
            className="mt-1.5 h-12 w-full rounded-xl border border-line px-4 text-[17px] outline-none focus:border-brand-500" />
          <span className="mt-1.5 block text-[13px] text-muted">При первом входе пароль — <b>12345</b>. Его можно сменить в профиле.</span>
        </label>
        {error && <div className="rounded-xl bg-red-50 p-3 text-sm text-red-800">{error}</div>}
        <button disabled={iin.length !== 12 || !password || m.isPending} className="h-13 w-full rounded-xl bg-brand-600 py-3.5 text-[17px] font-semibold text-white disabled:opacity-50">
          {m.isPending ? 'Вход…' : 'Войти'}
        </button>
      </form>
      <p className="mt-auto pt-8 text-center text-[13px] leading-relaxed text-muted">
        Преподаватели входят по ссылке-приглашению от администратора центра — она откроет кабинет в Telegram.
      </p>
    </div>
  )
}

// ---------------------------------------------------------------- общий каркас

type Tab<T extends string> = { id: T; label: string; icon: ReactNode; badge?: number }

function Shell<T extends string>({ title, subtitle, tabs, tab, onTab, children, top }: { title: string; subtitle?: ReactNode; tabs: Tab<T>[]; tab: T; onTab: (t: T) => void; children: ReactNode; top?: ReactNode }) {
  return (
    <div className="mx-auto flex h-full max-w-xl flex-col bg-page">
      <header className="border-b border-line bg-white px-4 pb-3 pt-4">
        <div className="truncate text-xl font-bold">{title}</div>
        {subtitle && <div className="truncate text-sm text-muted">{subtitle}</div>}
        {top}
      </header>
      <main className="flex-1 overflow-y-auto p-4 pb-6">{children}</main>
      <nav className="grid border-t border-line bg-white pb-[env(safe-area-inset-bottom)]" style={{ gridTemplateColumns: `repeat(${tabs.length}, 1fr)` }}>
        {tabs.map((t) => (
          <button key={t.id} onClick={() => onTab(t.id)} className={cx('relative flex h-16 flex-col items-center justify-center gap-1 text-[11px] font-medium', tab === t.id ? 'text-brand-600' : 'text-muted')}>
            {t.icon}{t.label}
            {!!t.badge && <span className="absolute right-[22%] top-2 min-w-5 rounded-full bg-red-500 px-1.5 text-[11px] font-bold leading-5 text-white">{t.badge}</span>}
          </button>
        ))}
      </nav>
    </div>
  )
}

const Section = ({ title, children, action }: { title: string; children: ReactNode; action?: ReactNode }) => (
  <section className="mb-5">
    <div className="mb-2 flex items-center justify-between px-1"><h2 className="text-[13px] font-semibold uppercase tracking-wide text-muted">{title}</h2>{action}</div>
    {children}
  </section>
)
const Box = ({ children, className, onClick }: { children: ReactNode; className?: string; onClick?: () => void }) => (
  <div onClick={onClick} className={cx('rounded-2xl border border-line bg-white p-4', onClick && 'cursor-pointer active:bg-gray-50', className)}>{children}</div>
)
const Tile = ({ label, value, tone }: { label: string; value: ReactNode; tone?: 'red' | 'green' }) => (
  <div className="rounded-2xl border border-line bg-white p-3">
    <div className="text-[12px] text-muted">{label}</div>
    <div className={cx('mt-0.5 text-[22px] font-bold tabular', tone === 'red' && 'text-red-600', tone === 'green' && 'text-green-700')}>{value}</div>
  </div>
)
const Empty = ({ children }: { children: ReactNode }) => <div className="rounded-2xl border border-dashed border-line p-6 text-center text-sm text-muted">{children}</div>

function byDay<T extends { starts_at: string }>(items: T[]): [string, T[]][] {
  const out = new Map<string, T[]>()
  for (const i of items) {
    const k = dayMonth(new Date(i.starts_at))
    out.set(k, [...(out.get(k) ?? []), i])
  }
  return [...out.entries()]
}

function lessonBadge(l: Lesson): [string, Tone] | null {
  const now = Date.now()
  if (l.status === 'cancelled') return ['Отменено', 'gray']
  if (l.status === 'done') return ['Проведено', 'green']
  if (l.status === 'needs_decision') return ['Уточняется', 'yellow']
  if (new Date(l.starts_at).getTime() <= now && new Date(l.ends_at).getTime() >= now) return ['Идёт сейчас', 'blue']
  if (new Date(l.ends_at).getTime() < now) return ['Требует отметки', 'red']
  return null
}

function LessonCard({ l, onClick, showTeacher }: { l: Lesson; onClick?: () => void; showTeacher?: boolean }) {
  const b = lessonBadge(l)
  return (
    <Box onClick={onClick} className={cx('flex gap-3', l.status === 'cancelled' && 'opacity-60')}>
      <div className="w-14 shrink-0 text-[15px] font-bold tabular">{time(l.starts_at)}<div className="text-[12px] font-normal text-muted">{time(l.ends_at)}</div></div>
      <div className="min-w-0 flex-1">
        <div className="flex items-start justify-between gap-2"><div className="truncate text-[15px] font-semibold">{l.group}</div>{b && <Badge tone={b[1]}>{b[0]}</Badge>}</div>
        <div className="truncate text-[13px] text-muted">{[showTeacher && l.teacher, l.classroom, l.topic].filter(Boolean).join(' · ') || ' '}</div>
        {l.meeting_url && l.status !== 'cancelled' && <a href={l.meeting_url} target="_blank" rel="noreferrer" onClick={(e) => e.stopPropagation()} className="mt-1 inline-block text-[13px] font-medium text-brand-600">Ссылка на онлайн-урок</a>}
      </div>
    </Box>
  )
}

function Schedule({ lessons, onOpen, showTeacher, empty }: { lessons: Lesson[]; onOpen?: (l: Lesson) => void; showTeacher?: boolean; empty: string }) {
  if (!lessons.length) return <Empty>{empty}</Empty>
  return <>{byDay(lessons).map(([day, ls]) => (
    <Section key={day} title={day}><div className="space-y-2">{ls.map((l) => <LessonCard key={l.id} l={l} showTeacher={showTeacher} onClick={onOpen && (() => onOpen(l))} />)}</div></Section>
  ))}</>
}

// ---------------------------------------------------------------- ученик / родитель

function PortalHome({ onLogout }: { onLogout: () => void }) {
  const me = useQuery({ queryKey: ['portal-me'], queryFn: () => portal<PortalMe>('GET', '/portal/me') })
  const [child, setChild] = useState('')
  const [tab, setTab] = useState<'schedule' | 'attendance' | 'tests' | 'payments' | 'profile'>('schedule')
  const [lesson, setLesson] = useState<Overview['attendance'][number] | null>(null)
  const childId = child || me.data?.children[0]?.id || ''
  const ov = useQuery({ queryKey: ['portal-overview', childId], queryFn: () => portal<Overview>('GET', '/portal/students/' + childId), enabled: !!childId })
  if (me.isError) return <Center><div>Сессия истекла. <button className="font-semibold text-brand-600" onClick={onLogout}>Войти снова</button></div></Center>
  if (!me.data) return <Spinner className="h-full" />
  const m = me.data
  const o = ov.data
  const tabs: Tab<typeof tab>[] = [
    { id: 'schedule', label: 'Расписание', icon: <CalendarDays className="size-5" /> },
    { id: 'attendance', label: 'Уроки', icon: <NotebookPen className="size-5" /> },
    ...(o?.tests?.length ? [{ id: 'tests' as const, label: 'Тесты', icon: <GraduationCap className="size-5" /> }] : []),
    { id: 'payments', label: 'Оплата', icon: <Wallet className="size-5" /> },
    { id: 'profile', label: 'Профиль', icon: <CircleUser className="size-5" /> },
  ]
  const chips = m.kind === 'parent' && m.children.length > 1 && (
    <div className="-mx-1 mt-3 flex gap-2 overflow-x-auto px-1">
      {m.children.map((c) => (
        <button key={c.id} onClick={() => setChild(c.id)} className={cx('h-9 shrink-0 rounded-full border px-4 text-sm font-medium', c.id === childId ? 'border-brand-600 bg-brand-600 text-white' : 'border-line bg-white')}>{c.full_name.split(' ').slice(0, 2).join(' ')}</button>
      ))}
    </div>
  )
  return (
    <Shell title={m.kind === 'parent' ? m.profile.full_name : m.profile.full_name} subtitle={m.school} tabs={tabs} tab={tab} onTab={setTab} top={chips}>
      {m.kind === 'parent' && m.children.length === 0 && tab !== 'profile' ? <Empty>К вашей карточке пока не привязаны дети. Обратитесь к администратору центра.</Empty>
        : !o && tab !== 'profile' ? <Spinner /> : (
          <>
            {tab === 'schedule' && o && (
              <>
                {m.kind === 'parent' && <div className="mb-3 px-1 text-[15px] font-semibold">{o.profile.full_name}</div>}
                <LastHomework o={o} onOpen={setLesson} />
                <Schedule lessons={o.upcoming} showTeacher empty="Занятий на ближайшие две недели нет" />
              </>
            )}
            {tab === 'attendance' && o && <Attendance o={o} onOpen={setLesson} />}
            {tab === 'tests' && o && <Tests o={o} />}
            {tab === 'payments' && o && <Payments o={o} />}
            {tab === 'profile' && <Profile me={m} o={o} onLogout={onLogout} />}
          </>
        )}
      {lesson && o && <LessonSheet l={lesson} results={o.tests.filter((r) => r.lesson_id === lesson.lesson_id)} onClose={() => setLesson(null)} />}
    </Shell>
  )
}

type PastLesson = Overview['attendance'][number]

function Attendance({ o, onOpen }: { o: Overview; onOpen: (l: PastLesson) => void }) {
  const s = o.attendance_90d
  const total = Object.values(s).reduce((a, b) => a + b, 0)
  const came = (s.present ?? 0) + (s.late ?? 0) + (s.trial ?? 0)
  return (
    <>
      <Section title="Посещаемость за 90 дней">
        <div className="grid grid-cols-3 gap-2">
          <Tile label="Посещаемость" value={total ? `${Math.round((came / total) * 100)}%` : '—'} tone={total && came / total < 0.7 ? 'red' : 'green'} />
          <Tile label="Пропуски" value={s.absent ?? 0} tone={s.absent ? 'red' : undefined} />
          <Tile label="Опоздания" value={s.late ?? 0} />
        </div>
      </Section>
      <Section title="Последние уроки">
        {o.attendance.length === 0 ? <Empty>Отметок пока нет</Empty> : (
          <Box className="divide-y divide-line !p-0">
            {o.attendance.map((a) => {
              const [l, , t] = attendanceStatus[a.status] ?? [a.status, '', 'gray']
              const res = o.tests.filter((r) => r.lesson_id === a.lesson_id)
              return (
                <button key={a.lesson_id} onClick={() => onOpen(a)} className="flex w-full items-start gap-3 px-4 py-3 text-left active:bg-gray-50">
                  <div className="w-12 shrink-0 pt-0.5 text-[13px] text-muted tabular">{dateShort(a.starts_at)}</div>
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-[15px]">{a.group}</div>
                    {a.topic && <div className="truncate text-[12px] text-muted">{a.topic}</div>}
                    {(a.homework || res.length > 0) && (
                      <div className="mt-1 flex flex-wrap gap-1.5">
                        {a.homework && <span className="rounded-md bg-amber-50 px-1.5 py-0.5 text-[11px] font-medium text-amber-800">ДЗ</span>}
                        {res.map((r) => <span key={r.id} className="rounded-md bg-gray-100 px-1.5 py-0.5 text-[11px] font-semibold" style={gradeStyle(r.grade?.color)}>{r.percent != null ? `${num(r.percent)}%` : r.grade?.name ?? r.test_type}</span>)}
                      </div>
                    )}
                  </div>
                  <Badge tone={t as Tone}>{l}</Badge>
                </button>
              )
            })}
          </Box>
        )}
      </Section>
    </>
  )
}

// Последнее домашнее задание — первым на главном экране.
function LastHomework({ o, onOpen }: { o: Overview; onOpen: (l: PastLesson) => void }) {
  const l = o.attendance.find((a) => a.homework)
  if (!l) return null
  return (
    <Section title="Домашнее задание">
      <Box onClick={() => onOpen(l)} className="border-amber-200 bg-amber-50/60">
        <div className="text-[13px] text-muted">{l.group} · {dateShort(l.starts_at)}{l.topic && ` · ${l.topic}`}</div>
        <div className="mt-1 whitespace-pre-line text-[15px]">{l.homework}</div>
      </Box>
    </Section>
  )
}

// Экран урока: тема, ДЗ, отметка и результаты тестов ученика.
function LessonSheet({ l, results, onClose }: { l: PastLesson; results: TestResult[]; onClose: () => void }) {
  const [label, , tone] = attendanceStatus[l.status] ?? [l.status, '', 'gray']
  return (
    <div className="fixed inset-0 z-50 flex justify-center bg-page">
      <div className="flex h-full w-full max-w-xl flex-col">
        <header className="flex items-start gap-3 border-b border-line bg-white px-4 pb-3 pt-4">
          <div className="min-w-0 flex-1">
            <div className="truncate text-xl font-bold">{l.group}</div>
            <div className="text-sm text-muted first-letter:uppercase">{dayMonth(new Date(l.starts_at))}, {time(l.starts_at)}{l.teacher && ` · ${l.teacher}`}</div>
          </div>
          <button onClick={onClose} className="flex size-10 items-center justify-center rounded-full hover:bg-gray-100" aria-label="Закрыть"><X className="size-5" /></button>
        </header>
        <main className="flex-1 space-y-4 overflow-y-auto p-4">
          <Box className="flex items-center justify-between"><span className="text-[15px]">Отметка</span><Badge tone={tone as Tone}>{label}</Badge></Box>
          <Section title="Тема урока"><Box className="flex gap-3 text-[15px]"><BookOpen className="mt-0.5 size-4 shrink-0 text-brand-600" />{l.topic ?? <span className="text-muted">Не указана</span>}</Box></Section>
          <Section title="Домашнее задание">
            <Box className={cx('whitespace-pre-line text-[15px]', l.homework && 'border-amber-200 bg-amber-50/60')}>{l.homework ?? <span className="text-muted">Не задано</span>}</Box>
          </Section>
          <Section title="Результаты за урок">
            {results.length === 0 ? <Empty>Баллов за этот урок нет</Empty> : <div className="space-y-2">{results.map((r) => <ResultCard key={r.id} r={r} />)}</div>}
          </Section>
        </main>
      </div>
    </div>
  )
}

function Tests({ o }: { o: Overview }) {
  return <div className="space-y-2">{o.tests.map((r) => <ResultCard key={r.id} r={r} />)}</div>
}

function ResultCard({ r }: { r: TestResult }) {
  return (
        <Box>
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <div className="text-[15px] font-semibold">{r.test_type}</div>
              <div className="text-[13px] text-muted">{date(r.taken_on)}{r.group && ` · ${r.group}`}</div>
            </div>
            <div className="shrink-0 text-right">
              {r.percent != null && <div className="text-[20px] font-bold tabular">{num(r.percent)}%</div>}
              {r.grade && <span className="rounded-full bg-gray-100 px-2.5 py-0.5 text-[12px] font-semibold" style={gradeStyle(r.grade.color)}>{r.grade.name}</span>}
            </div>
          </div>
          {r.scores.length > 0 && (
            <div className="mt-3 space-y-1.5">
              {r.scores.map((s) => (
                <div key={s.skill_id} className="flex items-center justify-between gap-2 text-[14px]">
                  <span className="text-muted">{s.skill}</span>
                  {s.grade ? <span className="rounded-full bg-gray-100 px-2 py-0.5 text-[12px] font-semibold" style={gradeStyle(s.grade.color)}>{s.grade.name}</span> : <span className="tabular"><b>{num(s.score)}</b> / {num(s.max_score)}</span>}
                </div>
              ))}
            </div>
          )}
          {r.comment && <div className="mt-3 rounded-xl bg-gray-50 p-3 text-[14px]">{r.comment}</div>}
        </Box>
  )
}

function Payments({ o }: { o: Overview }) {
  return (
    <>
      <Box className="mb-5">
        <div className="text-[13px] text-muted">{o.balance_tiyn < 0 ? 'Задолженность' : 'Баланс'}</div>
        <div className={cx('mt-1 text-[28px] font-bold tabular', o.balance_tiyn < 0 ? 'text-red-600' : 'text-ink')}>{money(Math.abs(o.balance_tiyn))}</div>
        {o.balance_tiyn < 0 && <div className="mt-1 text-[13px] text-muted">Оплатить можно у администратора центра или переводом по реквизитам центра.</div>}
      </Box>
      <Section title="Абонементы">
        {o.subscriptions.length === 0 ? <Empty>Действующих абонементов нет</Empty> : (
          <div className="space-y-2">
            {o.subscriptions.map((s, i) => (
              <Box key={i}>
                <div className="flex items-start justify-between gap-2"><div className="text-[15px] font-semibold">{s.name}</div>{s.status === 'frozen' && <Badge tone="blue">Заморожен</Badge>}</div>
                {s.lessons_total != null && (
                  <>
                    <div className="mt-3 h-2 overflow-hidden rounded-full bg-gray-200"><div className={cx('h-full rounded-full', (s.lessons_left ?? 0) <= 2 ? 'bg-amber-500' : 'bg-brand-600')} style={{ width: `${Math.min(100, (s.lessons_used / s.lessons_total) * 100)}%` }} /></div>
                    <div className="mt-1.5 text-[13px] text-muted">Осталось {s.lessons_left} {plural(s.lessons_left ?? 0, 'занятие', 'занятия', 'занятий')} из {s.lessons_total}</div>
                  </>
                )}
                <div className="mt-1 text-[13px] text-muted">{date(s.starts_on)} — {s.ends_on ? date(s.ends_on) : 'без срока'}</div>
              </Box>
            ))}
          </div>
        )}
      </Section>
    </>
  )
}

function Profile({ me, o, onLogout }: { me: PortalMe; o?: Overview; onLogout: () => void }) {
  return (
    <>
      <Box className="mb-5 flex items-center gap-3">
        <Avatar name={me.profile.full_name} size="lg" />
        <div className="min-w-0">
          <div className="truncate text-[17px] font-semibold">{me.profile.full_name}</div>
          <div className="text-[13px] text-muted">{me.kind === 'parent' ? 'Родитель' : `Ученик №${o?.profile.number ?? me.profile.number ?? ''}`}{o && me.kind === 'student' && ` · ${o.profile.branch}`}</div>
        </div>
      </Box>
      {o && o.groups.length > 0 && (
        <Section title={me.kind === 'parent' ? `Группы · ${o.profile.full_name}` : 'Мои группы'}>
          <Box className="divide-y divide-line !p-0">
            {o.groups.map((g) => <div key={g.id} className="px-4 py-3"><div className="text-[15px] font-medium">{g.name}</div><div className="text-[13px] text-muted">{g.course}{g.teacher && ` · ${g.teacher}`}</div></div>)}
          </Box>
        </Section>
      )}
      <Section title="Уведомления">
        <Box className="text-[14px]">
          {me.kind === 'parent' ? (me.profile.notifications ? 'Уведомления о посещаемости, расписании и оплатах приходят в Telegram.' : 'Откройте кабинет из Telegram-бота центра, чтобы получать уведомления.') : (me.telegram_linked ? 'Кабинет привязан к вашему Telegram.' : 'Откройте кабинет из Telegram-бота центра, чтобы входить без пароля.')}
        </Box>
      </Section>
      <PasswordForm />
      <button onClick={onLogout} className="mt-2 h-12 w-full rounded-xl border border-line bg-white text-[15px] font-semibold text-red-600">Выйти</button>
    </>
  )
}

function PasswordForm() {
  const [f, setF] = useState({ current_password: '', new_password: '' })
  const [msg, setMsg] = useState<[string, boolean] | null>(null)
  const m = useMutation({
    mutationFn: () => portal('POST', '/portal/password', f),
    onSuccess: () => { setMsg(['Пароль изменён', true]); setF({ current_password: '', new_password: '' }) },
    onError: (e) => setMsg([e instanceof ApiError && e.fields.some((x) => x.field === 'current_password') ? 'Неверный текущий пароль' : e instanceof ApiError && e.fields.length ? 'Новый пароль — минимум 6 символов' : (e as Error).message, false]),
  })
  const cls = 'h-12 w-full rounded-xl border border-line px-4 text-[15px] outline-none focus:border-brand-500'
  return (
    <Section title="Сменить пароль">
      <Box className="space-y-3">
        <input type="password" placeholder="Текущий пароль" autoComplete="current-password" value={f.current_password} onChange={(e) => setF({ ...f, current_password: e.target.value })} className={cls} />
        <input type="password" placeholder="Новый пароль (от 6 символов)" autoComplete="new-password" value={f.new_password} onChange={(e) => setF({ ...f, new_password: e.target.value })} className={cls} />
        {msg && <div className={cx('text-sm', msg[1] ? 'text-green-700' : 'text-red-700')}>{msg[0]}</div>}
        <button disabled={!f.current_password || !f.new_password || m.isPending} onClick={() => { setMsg(null); m.mutate() }} className="h-12 w-full rounded-xl bg-brand-600 text-[15px] font-semibold text-white disabled:opacity-50">Сохранить пароль</button>
      </Box>
    </Section>
  )
}

// ---------------------------------------------------------------- преподаватель

function TeacherHome() {
  const nav = useNavigate()
  const qc = useQueryClient()
  const [tab, setTab] = useState<'schedule' | 'mark' | 'history' | 'profile'>('schedule')
  const q = useQuery({ queryKey: ['teacher-cabinet'], queryFn: () => api.get<TeacherCabinet>('/teacher/cabinet') })
  useEffect(() => { qc.invalidateQueries({ queryKey: ['teacher-cabinet'] }) }, [qc])
  if (q.isError) return <Center><div>{(q.error as Error).message}</div></Center>
  if (!q.data) return <Spinner className="h-full" />
  const { profile: p, stats: s, schedule, to_mark, conducted } = q.data
  const open = (l: { id: string }) => nav(`lesson/${l.id}`)
  const tabs: Tab<typeof tab>[] = [
    { id: 'schedule', label: 'Расписание', icon: <CalendarDays className="size-5" /> },
    { id: 'mark', label: 'Отметить', icon: <ClipboardList className="size-5" />, badge: to_mark.length },
    { id: 'history', label: 'Проведённые', icon: <History className="size-5" /> },
    { id: 'profile', label: 'Профиль', icon: <BarChart3 className="size-5" /> },
  ]
  return (
    <Shell title={p.full_name} subtitle={[p.position, p.school].filter(Boolean).join(' · ')} tabs={tabs} tab={tab} onTab={setTab}>
      {tab === 'schedule' && (
        <>
          {to_mark.length > 0 && (
            <button onClick={() => setTab('mark')} className="mb-4 w-full rounded-2xl bg-red-50 p-4 text-left text-[14px] text-red-900">
              <b>{to_mark.length} {plural(to_mark.length, 'занятие', 'занятия', 'занятий')}</b> без отметки — отметьте посещаемость, тему, ДЗ и баллы.
            </button>
          )}
          <Schedule lessons={schedule} onOpen={open} empty="На ближайшую неделю занятий нет" />
        </>
      )}
      {tab === 'mark' && (to_mark.length === 0 ? <Empty>Все прошедшие занятия отмечены</Empty> : (
        <Section title="Требуют отметки"><div className="space-y-2">{to_mark.map((l) => <LessonCard key={l.id} l={l} onClick={() => open(l)} />)}</div></Section>
      ))}
      {tab === 'history' && (conducted.length === 0 ? <Empty>Проведённых занятий пока нет</Empty> : (
        <Section title="Последние проведённые">
          <Box className="divide-y divide-line !p-0">
            {conducted.map((c) => (
              <button key={c.id} onClick={() => open(c)} className="flex w-full items-center gap-3 px-4 py-3 text-left active:bg-gray-50">
                <div className="w-12 shrink-0 text-[13px] text-muted tabular">{dateShort(c.starts_at)}</div>
                <div className="min-w-0 flex-1"><div className="truncate text-[15px]">{c.group}</div><div className="truncate text-[12px] text-muted">{c.topic || 'Тема не указана'}</div></div>
                <div className="text-right text-[13px] tabular"><b>{c.present}</b>/{c.marked}<div className="text-[11px] text-muted">пришли</div></div>
              </button>
            ))}
          </Box>
        </Section>
      ))}
      {tab === 'profile' && (
        <>
          <TeacherPayroll />
          <Section title="Статистика">
            <div className="grid grid-cols-2 gap-2">
              <Tile label="Проведено в этом месяце" value={s.lessons_month} />
              <Tile label="Всего проведено" value={s.lessons_total} />
              <Tile label="Посещаемость за 30 дней" value={`${s.attendance_rate_30d}%`} tone={s.attendance_rate_30d < 70 ? 'red' : 'green'} />
              <Tile label="Без отметки" value={s.unmarked} tone={s.unmarked ? 'red' : undefined} />
              <Tile label="Группы" value={s.groups} />
              <Tile label="Ученики" value={s.students} />
            </div>
          </Section>
          <Section title="Обо мне">
            <Box className="space-y-2 text-[14px]">
              {([['Телефон', p.phone], ['Email', p.email?.endsWith('@cabinet.local') ? null : p.email], ['Предметы', p.subjects.join(', ')], ['Филиалы', p.branches.join(', ')], ['Работает с', p.hired_at && date(p.hired_at)]] as [string, string | null][])
                .filter(([, v]) => v).map(([l, v]) => <div key={l} className="flex justify-between gap-3"><span className="text-muted">{l}</span><span className="text-right">{v}</span></div>)}
            </Box>
          </Section>
        </>
      )}
    </Shell>
  )
}

// Начисления за месяц — те же данные, что в разделе «Зарплата» у администратора.
function TeacherPayroll() {
  const [month, setMonth] = useState(() => new Date().toISOString().slice(0, 7))
  const [open, setOpen] = useState(false)
  const q = useQuery({ queryKey: ['teacher-payroll', month], queryFn: () => api.get<Payroll>('/teacher/payroll?month=' + month) })
  const shift = (d: number) => {
    const [y, m] = month.split('-').map(Number)
    const t = new Date(Date.UTC(y, m - 1 + d, 1))
    setMonth(t.toISOString().slice(0, 7))
  }
  const label = new Date(month + '-01T00:00:00').toLocaleDateString('ru-RU', { month: 'long', year: 'numeric' })
  const r = q.data
  const t = r?.total
  const pay = r?.teachers[0]
  return (
    <Section title="Начисления" action={
      <div className="flex items-center gap-1 text-[13px]">
        <button onClick={() => shift(-1)} className="h-8 rounded-lg px-2 text-brand-600" aria-label="Предыдущий месяц">‹</button>
        <span className="min-w-28 text-center font-medium first-letter:uppercase">{label}</span>
        <button onClick={() => shift(1)} disabled={month >= new Date().toISOString().slice(0, 7)} className="h-8 rounded-lg px-2 text-brand-600 disabled:opacity-30" aria-label="Следующий месяц">›</button>
      </div>}>
      {!r || !t ? <Spinner /> : (
        <>
          <Box className="mb-2">
            <div className="text-[13px] text-muted">Начислено за проведённые занятия</div>
            <div className="mt-1 text-[28px] font-bold tabular">{money(t.amount_tiyn)}</div>
            {pay && <div className="mt-1 text-[13px] text-muted">{payTypes[pay.pay_type]} · {rateText(pay.pay_type, pay.rate_tiyn)}{pay.salary_tiyn > 0 && ' — оклад начисляется отдельно'}</div>}
          </Box>
          <div className="mb-2 grid grid-cols-3 gap-2">
            <Tile label="Занятий" value={t.lessons} />
            <Tile label="Часов" value={hours(t.minutes)} />
            <Tile label="Замен" value={t.substitutions} />
          </div>
          <div className="mb-2 grid grid-cols-4 gap-2 text-center">
            {([['Был', t.present], ['Опоздал', t.late], ['Не был', t.absent], ['Уваж.', t.excused]] as [string, number][]).map(([l, v]) => (
              <div key={l} className="rounded-xl border border-line bg-white py-2"><div className="text-[17px] font-bold tabular">{v}</div><div className="text-[11px] text-muted">{l}</div></div>
            ))}
          </div>
          {r.lessons.length > 0 && (
            <>
              <button onClick={() => setOpen(!open)} className="mb-2 h-11 w-full rounded-xl border border-line bg-white text-[14px] font-semibold text-brand-600">
                {open ? 'Скрыть расчёт по занятиям' : `Расчёт по занятиям (${r.lessons.length})`}
              </button>
              {open && (
                <Box className="divide-y divide-line !p-0">
                  {r.lessons.map((l) => (
                    <div key={l.lesson_id} className="px-4 py-3">
                      <div className="flex items-start justify-between gap-3">
                        <div className="min-w-0">
                          <div className="truncate text-[15px] font-medium">{l.group}</div>
                          <div className="text-[12px] text-muted">{dateShort(l.starts_at)} {time(l.starts_at)} · {l.minutes} мин · Б {l.present} / О {l.late} / Н {l.absent} / У {l.excused}</div>
                          {l.substitute_for && <div className="text-[12px] text-purple-700">замена за {l.substitute_for}</div>}
                        </div>
                        <div className="shrink-0 text-[15px] font-bold tabular">{money(l.amount_tiyn)}</div>
                      </div>
                      <div className="mt-1 text-[12px] text-gray-600">{l.formula}</div>
                    </div>
                  ))}
                </Box>
              )}
            </>
          )}
        </>
      )}
    </Section>
  )
}
