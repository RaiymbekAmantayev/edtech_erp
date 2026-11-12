// Локализация интерфейса: RU (основной), KZ, EN. Тексты без перевода показываются на русском.
import { useSyncExternalStore } from 'react'
import kk from './i18n.kk'

export type Lang = 'ru' | 'kk' | 'en'

const dict: Record<string, Partial<Record<Lang, string>>> = {
  'nav.home': { kk: 'Басты бет', en: 'Home' },
  'nav.leads': { kk: 'Лидтер', en: 'Leads' },
  'nav.students': { kk: 'Оқушылар', en: 'Students' },
  'nav.parents': { kk: 'Ата-аналар', en: 'Parents' },
  'nav.teachers': { kk: 'Оқытушылар', en: 'Teachers' },
  'nav.groups': { kk: 'Топтар мен курстар', en: 'Groups & courses' },
  'nav.schedule': { kk: 'Кесте', en: 'Schedule' },
  'nav.attendance': { kk: 'Қатысу', en: 'Attendance' },
  'nav.finance': { kk: 'Қаржы', en: 'Finance' },
  'nav.reports': { kk: 'Есептер', en: 'Reports' },
  'nav.settings': { kk: 'Баптаулар', en: 'Settings' },
  'nav.my': { kk: 'Менің сабақтарым', en: 'My lessons' },
  'header.branch': { kk: 'Филиал', en: 'Branch' },
  'header.allBranches': { kk: 'Барлық филиалдар', en: 'All branches' },
  'header.search': { kk: 'Оқушы, ата-ана, топ немесе телефон іздеу', en: 'Search students, parents, groups or phone' },
  'header.create': { kk: 'Құру', en: 'Create' },
  'auth.login': { kk: 'Кіру', en: 'Sign in' },
  'auth.email': { kk: 'Жұмыс email', en: 'Work email' },
  'auth.password': { kk: 'Құпия сөз', en: 'Password' },
  'auth.title': { kk: 'Оқу орталығы бір жүйеде', en: 'Your learning center in one system' },
  'auth.subtitle': { kk: 'Оқушылар, кесте, қатысу, төлемдер және Telegram-дағы ата-аналарға хабарламалар.', en: 'Students, schedule, attendance, payments and Telegram notifications for parents.' },
  'common.save': { kk: 'Сақтау', en: 'Save' },
  'common.cancel': { kk: 'Болдырмау', en: 'Cancel' },
  'common.logout': { kk: 'Шығу', en: 'Log out' },
  'dash.hello': { kk: 'Қайырлы күн', en: 'Good afternoon' },
}

const stored = (() => { try { return localStorage.getItem('emp.lang') as Lang | null } catch { return null } })()
// без сохранённого выбора — язык браузера/Telegram (kk → казахский), иначе русский
let lang: Lang = stored || (navigator.language?.toLowerCase().startsWith('kk') ? 'kk' : 'ru')
document.documentElement.lang = lang
const subs = new Set<() => void>()

export const getLang = () => lang

// Смена языка перезагружает страницу: так переводятся и тексты, вычисленные при загрузке модулей (статусы, меню, справочники).
export function setLang(l: Lang) {
  if (l === lang) return
  lang = l
  try { localStorage.setItem('emp.lang', l) } catch { /* приватный режим */ }
  document.documentElement.lang = l
  subs.forEach((s) => s())
  window.location.reload()
}

// tr('Текст на русском', ...аргументы) — перевод по русскому тексту; {0}, {1} подставляются из аргументов.
// Нет перевода — показывается русский текст. Словарь KZ: i18n.kk.ts (проверка полноты: node scripts/i18n-check.mjs).
export function tr(ru: string, ...args: unknown[]): string {
  let s = lang === 'kk' ? kk[ru] ?? ru : ru
  for (let i = 0; i < args.length; i++) s = s.split(`{${i}}`).join(String(args[i] ?? ''))
  return s
}

// Локаль для дат и чисел (Intl).
export const intlLocale = () => (lang === 'kk' ? 'kk-KZ' : lang === 'en' ? 'en-GB' : 'ru-RU')

export function useLang(): [Lang, (key: string, ru: string) => string] {
  const l = useSyncExternalStore((cb) => { subs.add(cb); return () => subs.delete(cb) }, () => lang)
  return [l, (key, ru) => (l === 'ru' ? ru : dict[key]?.[l] ?? ru)]
}

export const label = (m: Record<string, string> | undefined, l: Lang = lang) => (m ? m[l] || m.ru || '' : '')
