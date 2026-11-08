import { NavLink, Navigate, Route, Routes } from 'react-router-dom'
import { cx } from '../../components/ui'
import CustomFields from './CustomFields'
import { Branches, Calendar, Organization, Plan, Rules } from './General'
import TelegramBot from './TelegramBot'
import Users from './Users'

const items = [
  ['organization', 'Организация'], ['branches', 'Филиалы и кабинеты'], ['users', 'Пользователи и роли'], ['fields', 'Пользовательские поля'],
  ['rules', 'Правила списания'], ['calendar', 'Календарь и праздники'], ['telegram', 'Telegram-бот'], ['plan', 'Тариф и оплата'],
]
const soon = [['Статусы и воронки', 'этап 6'], ['Шаблоны документов', 'этап 6'], ['WhatsApp и SMS', 'этап 7']]

export default function Settings() {
  return (
    <div className="flex min-h-full">
      <aside className="hidden w-56 shrink-0 border-r border-line bg-white px-3 py-6 md:block">
        <div className="px-3 pb-3 text-[11px] font-semibold uppercase tracking-wide text-muted">Настройки</div>
        {items.map(([to, l]) => (
          <NavLink key={to} to={to} className={({ isActive }) => cx('block rounded-lg px-3 py-2 text-sm', isActive ? 'bg-brand-50 font-medium text-brand-700' : 'text-ink hover:bg-gray-50')}>{l}</NavLink>
        ))}
        <div className="mt-2 space-y-0.5">
          {soon.map(([l, s]) => <div key={l} className="flex justify-between px-3 py-2 text-sm text-gray-400" title="Запланировано в roadmap">{l}<span className="text-[10px]">{s}</span></div>)}
        </div>
      </aside>
      <div className="min-w-0 flex-1 p-4 lg:p-8">
        <Routes>
          <Route index element={<Navigate to="fields" replace />} />
          <Route path="organization" element={<Organization />} />
          <Route path="branches" element={<Branches />} />
          <Route path="users" element={<Users />} />
          <Route path="fields" element={<CustomFields />} />
          <Route path="rules" element={<Rules />} />
          <Route path="calendar" element={<Calendar />} />
          <Route path="telegram" element={<TelegramBot />} />
          <Route path="plan" element={<Plan />} />
        </Routes>
      </div>
    </div>
  )
}
