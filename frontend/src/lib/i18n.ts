// Локализация интерфейса: RU (основной), KZ, EN. Тексты без перевода показываются на русском.
import { useSyncExternalStore } from 'react'

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

let lang: Lang = (localStorage.getItem('emp.lang') as Lang) || 'ru'
const subs = new Set<() => void>()

export function setLang(l: Lang) {
  lang = l
  localStorage.setItem('emp.lang', l)
  document.documentElement.lang = l === 'kk' ? 'kk' : l
  subs.forEach((s) => s())
}

export function useLang(): [Lang, (key: string, ru: string) => string] {
  const l = useSyncExternalStore((cb) => { subs.add(cb); return () => subs.delete(cb) }, () => lang)
  return [l, (key, ru) => (l === 'ru' ? ru : dict[key]?.[l] ?? ru)]
}

export const label = (m: Record<string, string> | undefined, l: Lang = lang) => (m ? m[l] || m.ru || '' : '')
