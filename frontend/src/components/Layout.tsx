import { useEffect, useRef, useState } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Bell, CalendarDays, CheckSquare, ClipboardCheck, CreditCard, Filter, GraduationCap, Home, LayoutGrid, LogOut, Plus, Search, Settings, BarChart3, UserRound, Users, Menu, Wallet } from 'lucide-react'
import { useAuth } from '../lib/auth'
import { setLang, useLang, type Lang, tr, intlLocale } from '../lib/i18n'
import { api } from '../lib/api'
import { cx, Select } from './ui'
import { SearchPalette } from './SearchPalette'
import { StudentFormModal } from '../pages/StudentForm'
import { PaymentModal } from '../pages/finance/PaymentModal'
import { LeadFormModal } from '../pages/leads/LeadForm'

export function Logo({ name, platform }: { name?: string; platform?: boolean }) {
  return (
    <div className="flex items-center gap-2.5">
      <div className={cx('flex size-9 items-center justify-center rounded-lg text-sm font-bold text-white', platform ? 'bg-platform' : 'bg-brand-600')}>E</div>
      <div className="leading-tight">
        <div className="text-[15px] font-semibold text-white">EMP{platform && <span className="ml-1 font-normal text-teal-300">{tr('Платформа')}</span>}</div>
        <div className="max-w-36 truncate text-xs text-gray-400">{platform ? 'Super Admin' : name}</div>
      </div>
    </div>
  )
}

export default function Layout() {
  const { me, can, logout, branchId, setBranchId, isTeacherOnly } = useAuth()
  const [, t] = useLang()
  const [search, setSearch] = useState(false)
  const [create, setCreate] = useState<null | 'menu' | 'student' | 'payment' | 'lead'>(null)
  const [mobileNav, setMobileNav] = useState(false)
  const nav = useNavigate()
  const leads = useQuery({ queryKey: ['leads-new-count'], queryFn: () => api.getMeta('/leads?stage=new&limit=1'), enabled: can('leads.view'), refetchInterval: 60000 })
  const newLeads = (leads.data?.meta.counts as Record<string, number> | undefined)?.new ?? 0

  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setSearch(true) }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [])

  const items = isTeacherOnly
    ? [{ to: '/', icon: Home, label: t('nav.my', 'Мои занятия') }, { to: '/schedule', icon: CalendarDays, label: t('nav.schedule', 'Расписание') }, { to: '/attendance', icon: CheckSquare, label: t('nav.attendance', 'Посещаемость') }]
    : [
        { to: '/', icon: Home, label: t('nav.home', 'Главная') },
        can('leads.view') && { to: '/leads', icon: Filter, label: t('nav.leads', 'Лиды'), badge: newLeads },
        can('students.view') && { to: '/students', icon: Users, label: t('nav.students', 'Ученики') },
        can('parents.view') && { to: '/parents', icon: UserRound, label: t('nav.parents', 'Родители') },
        can('employees.view') && { to: '/teachers', icon: GraduationCap, label: t('nav.teachers', 'Преподаватели') },
        can('groups.view') && { to: '/groups', icon: LayoutGrid, label: t('nav.groups', 'Группы и курсы') },
        (can('schedule.view') || can('attendance.mark')) && { to: '/schedule', icon: CalendarDays, label: t('nav.schedule', 'Расписание') },
        (can('attendance.view') || can('attendance.mark')) && { to: '/attendance', icon: CheckSquare, label: t('nav.attendance', 'Посещаемость') },
        can('students.view') && { to: '/tests', icon: ClipboardCheck, label: t('nav.tests', 'Тесты учеников') },
        can('finance.view') && { to: '/finance', icon: CreditCard, label: t('nav.finance', 'Финансы') },
        can('payroll.view') && { to: '/payroll', icon: Wallet, label: t('nav.payroll', 'Зарплата') },
        can('reports.view') && { to: '/reports', icon: BarChart3, label: t('nav.reports', 'Отчёты') },
      ].filter(Boolean) as { to: string; icon: typeof Home; label: string; badge?: number }[]

  const sidebar = (
    <aside className="flex h-full w-60 shrink-0 flex-col bg-side px-3 py-5">
      <div className="px-2 pb-6"><Logo name={me?.tenant?.name} /></div>
      <nav className="flex flex-1 flex-col gap-0.5">
        {items.map((i) => (
          <NavLink key={i.to} to={i.to} end={i.to === '/'} onClick={() => setMobileNav(false)}
            className={({ isActive }) => cx('flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium', isActive ? 'bg-side-hover text-white' : 'text-gray-300 hover:bg-side-hover hover:text-white')}>
            <i.icon className="size-[18px]" strokeWidth={1.8} />
            <span className="flex-1">{i.label}</span>
            {!!i.badge && <span className="rounded-full bg-brand-600 px-2 py-0.5 text-xs font-semibold text-white">{i.badge}</span>}
          </NavLink>
        ))}
        {can('settings.manage') && (
          <NavLink to="/settings" onClick={() => setMobileNav(false)}
            className={({ isActive }) => cx('mt-4 flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium', isActive ? 'bg-side-hover text-white' : 'text-gray-300 hover:bg-side-hover hover:text-white')}>
            <Settings className="size-[18px]" strokeWidth={1.8} />{t('nav.settings', 'Настройки')}
          </NavLink>
        )}
      </nav>
      {me?.tenant?.status === 'trial' && me.tenant.trial_ends_at && (
        <div className="mx-1 rounded-lg bg-white/5 p-3 text-xs text-gray-300">
          {tr('Пробный период до')} {new Date(me.tenant.trial_ends_at).toLocaleDateString(intlLocale())}
        </div>
      )}
    </aside>
  )

  return (
    <div className="flex h-full">
      <div className="hidden lg:block">{sidebar}</div>
      {mobileNav && <div className="fixed inset-0 z-40 flex bg-black/40 lg:hidden" onClick={() => setMobileNav(false)}><div onClick={(e) => e.stopPropagation()}>{sidebar}</div></div>}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-16 shrink-0 items-center gap-3 border-b border-line bg-white px-4 lg:px-6">
          <button className="rounded-md p-2 lg:hidden" onClick={() => setMobileNav(true)} aria-label={tr('Меню')}><Menu className="size-5" /></button>
          {(me?.branches?.length ?? 0) > 1 && !isTeacherOnly && (
            <div className="hidden items-center gap-2 md:flex">
              <span className="text-[13px] text-muted">{t('header.branch', 'Филиал')}</span>
              <Select value={branchId} onChange={(e) => setBranchId(e.target.value)} className="!h-9 w-48">
                {me?.all_branches && <option value="">{t('header.allBranches', 'Все филиалы')}</option>}
                {me?.branches?.map((b) => <option key={b.id} value={b.id}>{b.name}</option>)}
              </Select>
            </div>
          )}
          <button onClick={() => setSearch(true)} className="flex h-9 min-w-0 flex-1 items-center gap-2 rounded-lg border border-line bg-gray-50 px-3 text-left text-sm text-gray-400 hover:border-gray-300 md:max-w-xl">
            <Search className="size-4" />
            <span className="flex-1 truncate">{t('header.search', 'Поиск ученика, родителя, группы или телефона')}</span>
            <kbd className="hidden rounded border border-line bg-white px-1.5 text-[11px] text-muted sm:inline">Ctrl K</kbd>
          </button>
          <div className="hidden flex-1 md:block" />
          {!isTeacherOnly && (can('students.edit') || can('payments.create') || can('leads.edit')) && (
            <div className="relative">
              <button onClick={() => setCreate(create === 'menu' ? null : 'menu')} className="flex h-9 items-center gap-1.5 rounded-lg bg-brand-600 px-3.5 text-sm font-medium text-white hover:bg-brand-700">
                <Plus className="size-4" />{t('header.create', 'Создать')}
              </button>
              {create === 'menu' && (
                <Dropdown onClose={() => setCreate(null)}>
                  {can('students.edit') && <MenuItem onClick={() => setCreate('student')}>{tr('Ученик')}</MenuItem>}
                  {can('payments.create') && <MenuItem onClick={() => setCreate('payment')}>{tr('Платёж')}</MenuItem>}
                  {can('leads.edit') && <MenuItem onClick={() => setCreate('lead')}>{tr('Лид')}</MenuItem>}
                  {can('schedule.edit') && <MenuItem onClick={() => { setCreate(null); nav('/schedule?new=1') }}>{tr('Занятие')}</MenuItem>}
                  {can('groups.edit') && <MenuItem onClick={() => { setCreate(null); nav('/groups?new=group') }}>{tr('Группа')}</MenuItem>}
                </Dropdown>
              )}
            </div>
          )}
          <button className="flex size-9 items-center justify-center rounded-lg border border-line text-gray-600 hover:bg-gray-50" aria-label={tr('Уведомления')} onClick={() => nav('/')}>
            <Bell className="size-[18px]" />
          </button>
          <UserMenu name={me?.user.full_name ?? ''} roles={me?.roles ?? []} onLogout={logout} />
        </header>
        <main className="min-h-0 flex-1 overflow-y-auto"><Outlet /></main>
      </div>
      {search && <SearchPalette onClose={() => setSearch(false)} />}
      {create === 'student' && <StudentFormModal onClose={() => setCreate(null)} onCreated={(id) => { setCreate(null); nav('/students/' + id) }} />}
      {create === 'payment' && <PaymentModal onClose={() => setCreate(null)} />}
      {create === 'lead' && <LeadFormModal onClose={() => setCreate(null)} />}
    </div>
  )
}

export function Dropdown({ children, onClose, align = 'right' }: { children: React.ReactNode; onClose: () => void; align?: 'right' | 'left' }) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const h = (e: MouseEvent) => ref.current && !ref.current.contains(e.target as Node) && onClose()
    setTimeout(() => document.addEventListener('mousedown', h))
    return () => document.removeEventListener('mousedown', h)
  }, [onClose])
  return <div ref={ref} className={cx('absolute top-11 z-30 min-w-48 rounded-xl border border-line bg-white py-1.5 shadow-lg', align === 'right' ? 'right-0' : 'left-0')}>{children}</div>
}
export const MenuItem = ({ children, onClick, danger }: { children: React.ReactNode; onClick: () => void; danger?: boolean }) => (
  <button onClick={onClick} className={cx('flex w-full items-center gap-2 px-4 py-2 text-left text-sm hover:bg-gray-50', danger && 'text-red-700')}>{children}</button>
)

const roleNames: Record<string, string> = { owner: tr('Владелец'), org_admin: tr('Администратор'), branch_manager: tr('Управляющий'), academic: tr('Учебный отдел'), accountant: tr('Бухгалтер'), hr: 'HR', sales: tr('Продажи'), teacher: tr('Преподаватель') }

function UserMenu({ name, roles, onLogout }: { name: string; roles: string[]; onLogout: () => void }) {
  const [open, setOpen] = useState(false)
  const [lang, t] = useLang()
  const init = name.split(' ').map((p) => p[0]).slice(0, 2).join('')
  return (
    <div className="relative">
      <button onClick={() => setOpen(!open)} className="flex size-9 items-center justify-center rounded-full bg-brand-100 text-sm font-semibold text-brand-700">{init}</button>
      {open && (
        <Dropdown onClose={() => setOpen(false)}>
          <div className="border-b border-line px-4 pb-2.5 pt-1.5">
            <div className="text-sm font-semibold">{name}</div>
            <div className="text-xs text-muted">{roles.map((r) => roleNames[r] ?? r).join(', ')}</div>
          </div>
          <div className="flex gap-1 px-4 py-2.5">
            {(['ru', 'kk', 'en'] as Lang[]).map((l) => (
              <button key={l} onClick={() => setLang(l)} className={cx('rounded-md border px-2.5 py-1 text-xs font-semibold', lang === l ? 'border-ink bg-ink text-white' : 'border-line')}>
                {l === 'kk' ? tr('ҚАЗ') : l.toUpperCase()}
              </button>
            ))}
          </div>
          <MenuItem onClick={onLogout}><LogOut className="size-4" />{t('common.logout', 'Выйти')}</MenuItem>
        </Dropdown>
      )}
    </div>
  )
}
