import { NavLink, Navigate, Route, Routes } from 'react-router-dom'
import { cx } from '../../components/ui'
import CustomFields from './CustomFields'
import { Branches, Calendar, Organization, Plan, Rules } from './General'
import TelegramBot from './TelegramBot'
import Testing from './Testing'
import Ktp from './Ktp'
import Users from './Users'
import PayrollSettingsPage from './Payroll'
import { tr } from '../../lib/i18n'

const items = [
  ['organization', tr('Организация')], ['branches', tr('Филиалы и кабинеты')], ['users', tr('Пользователи и роли')], ['fields', tr('Пользовательские поля')],
  ['testing', tr('Тестирование')], ['ktp', tr('КТП')], ['rules', tr('Правила списания')], ['payroll', tr('Зарплата')], ['calendar', tr('Календарь и праздники')], ['telegram', tr('Telegram-бот')], ['plan', tr('Тариф и оплата')],
]
const soon = [[tr('Статусы и воронки'), tr('этап 6')], [tr('Шаблоны документов'), tr('этап 6')], [tr('WhatsApp и SMS'), tr('этап 7')]]

export default function Settings() {
  return (
    <div className="flex min-h-full">
      <aside className="hidden w-56 shrink-0 border-r border-line bg-white px-3 py-6 md:block">
        <div className="px-3 pb-3 text-[11px] font-semibold uppercase tracking-wide text-muted">{tr('Настройки')}</div>
        {items.map(([to, l]) => (
          <NavLink key={to} to={`/settings/${to}`} className={({ isActive }) => cx('block rounded-lg px-3 py-2 text-sm', isActive ? 'bg-brand-50 font-medium text-brand-700' : 'text-ink hover:bg-gray-50')}>{l}</NavLink>
        ))}
        <div className="mt-2 space-y-0.5">
          {soon.map(([l, s]) => <div key={l} className="flex justify-between px-3 py-2 text-sm text-gray-400" title={tr('Запланировано в roadmap')}>{l}<span className="text-[10px]">{s}</span></div>)}
        </div>
      </aside>
      <div className="min-w-0 flex-1 p-4 lg:p-8">
        <Routes>
          <Route index element={<Navigate to="fields" replace />} />
          <Route path="organization" element={<Organization />} />
          <Route path="branches" element={<Branches />} />
          <Route path="users" element={<Users />} />
          <Route path="fields" element={<CustomFields />} />
          <Route path="testing" element={<Testing />} />
          <Route path="ktp" element={<Ktp />} />
          <Route path="rules" element={<Rules />} />
          <Route path="payroll" element={<PayrollSettingsPage />} />
          <Route path="calendar" element={<Calendar />} />
          <Route path="telegram" element={<TelegramBot />} />
          <Route path="plan" element={<Plan />} />
        </Routes>
      </div>
    </div>
  )
}
