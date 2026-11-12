import { NavLink, Outlet } from 'react-router-dom'
import { Activity, Building2, CalendarDays, LogOut, SlidersHorizontal } from 'lucide-react'
import { useAuth } from '../../lib/auth'
import { cx } from '../../components/ui'
import { Logo } from '../../components/Layout'
import { tr } from '../../lib/i18n'

export default function PlatformLayout() {
  const { logout } = useAuth()
  const items = [
    { to: '/platform', icon: Building2, label: tr('Организации'), end: true },
    { to: '/platform/plans', icon: SlidersHorizontal, label: tr('Тарифы и лимиты') },
    { to: '/platform/system', icon: Activity, label: tr('Мониторинг системы') },
    { to: '/platform/holidays', icon: CalendarDays, label: tr('Праздники и справочники') },
  ]
  return (
    <div className="flex h-full">
      <aside className="hidden w-60 shrink-0 flex-col bg-side px-3 py-5 lg:flex">
        <div className="px-2 pb-6"><Logo platform /></div>
        <nav className="flex flex-1 flex-col gap-0.5">
          {items.map((i) => (
            <NavLink key={i.to} to={i.to} end={i.end} className={({ isActive }) => cx('flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium', isActive ? 'bg-side-hover text-white' : 'text-gray-300 hover:bg-side-hover hover:text-white')}>
              <i.icon className="size-[18px]" strokeWidth={1.8} />{i.label}
            </NavLink>
          ))}
        </nav>
        <div className="mx-1 mb-3 rounded-lg bg-white/5 p-3 text-xs leading-relaxed text-gray-400">
          {tr('Персональные данные учеников здесь не показываются — только агрегаты. Доступ к данным организации — по разрешению владельца.')}
        </div>
        <button onClick={logout} className="flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm text-gray-300 hover:bg-side-hover hover:text-white"><LogOut className="size-[18px]" />{tr('Выйти')}</button>
      </aside>
      <main className="min-w-0 flex-1 overflow-y-auto"><Outlet /></main>
    </div>
  )
}
