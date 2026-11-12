import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ExternalLink } from 'lucide-react'
import { ApiError, api } from '../../lib/api'
import { dateTime, plural } from '../../lib/format'
import type { LessonMode, TestConfig } from '../../lib/tests'
import { LessonModeSelect } from '../../components/Tests'
import { Badge, Banner, Button, Card, Checkbox, Field, Input, Spinner, Stat, Textarea, Toast } from '../../components/ui'
import { tr } from '../../lib/i18n'

type Bot = {
  connected: boolean
  username?: string
  enabled?: boolean
  welcome_text?: string | null
  last_error?: string | null
  updated_at?: string
  notify: Record<string, boolean>
  miniapp_url: string
  platform_bot: string | null
  stats: { parents_notified: number; portal_accounts: number; staff_linked: number }
}

const notifyLabels: Record<string, string> = {
  attendance: tr('Пропуск и опоздание ученика'),
  lessons: tr('Отмена и перенос занятия'),
  homework: tr('Итог урока: тема, ДЗ и результаты тестов'),
  subscription: tr('Абонемент заканчивается'),
  payment: tr('Оплата получена'),
}

// Настройки Telegram-бота школы: свой бот из @BotFather, приветствие, типы уведомлений, ссылка на кабинет.
export default function TelegramBot() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['telegram-bot'], queryFn: () => api.get<Bot>('/settings/telegram-bot') })
  const [token, setToken] = useState('')
  const [welcome, setWelcome] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [toast, setToast] = useState('')
  const save = useMutation({
    mutationFn: (body: Record<string, unknown>) => api.put<Bot>('/settings/telegram-bot', body),
    onSuccess: (d) => { qc.setQueryData(['telegram-bot'], d); setToken(''); setWelcome(null); setError(''); setToast(tr('Сохранено')) },
    onError: (e) => setError(e instanceof ApiError && e.fields.some((f) => f.field === 'token') ? tr('Telegram не принял токен — проверьте, что он скопирован целиком') : (e as Error).message),
  })
  const del = useMutation({
    mutationFn: () => api.del('/settings/telegram-bot'),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['telegram-bot'] }),
  })
  if (!q.data) return <Spinner />
  const b = q.data
  return (
    <div className="max-w-3xl space-y-5">
      <div>
        <h1 className="text-[22px] font-bold">{tr('Telegram-бот')}</h1>
        <p className="mt-1 text-sm text-muted">{tr('Бот школы присылает родителям уведомления и открывает личный кабинет учеников, родителей и преподавателей.')}</p>
      </div>

      <div className="grid grid-cols-3 gap-3">
        <Stat label={tr('Родители получают уведомления')} value={b.stats.parents_notified} />
        <Stat label={tr('Кабинеты учеников и родителей')} value={b.stats.portal_accounts} />
        <Stat label={tr('Сотрудники с доступом в Telegram')} value={b.stats.staff_linked} />
      </div>

      {b.connected ? (
        <Card title={<span className="flex items-center gap-2">@{b.username} {b.enabled ? <Badge tone="green">{tr('Работает')}</Badge> : <Badge>{tr('Выключен')}</Badge>}</span>}
          action={<a href={`https://t.me/${b.username}`} target="_blank" rel="noreferrer" className="flex items-center gap-1 text-sm text-brand-600">{tr('Открыть')} <ExternalLink className="size-3.5" /></a>}>
          <div className="space-y-4 text-sm">
            {b.last_error && <Banner tone="orange">{b.last_error}</Banner>}
            <div className="text-muted">{tr('Обновлён')} {dateTime(b.updated_at)}{tr('. Кнопка «Кабинет» в меню бота открывает личный кабинет.')}</div>
            <div className="flex flex-wrap gap-2">
              <Button onClick={() => save.mutate({ enabled: !b.enabled })}>{b.enabled ? tr('Выключить бота') : tr('Включить бота')}</Button>
              <Button variant="danger" onClick={() => { if (confirm(tr('Отключить бота? Родители, привязанные через него, перестанут получать уведомления, пока не привяжутся заново.'))) del.mutate() }}>{tr('Отключить')}</Button>
            </div>
          </div>
        </Card>
      ) : (
        <Banner tone="blue">
          {b.platform_bot ? <>{tr('Сейчас используется общий бот платформы @')}{b.platform_bot}{tr('. Подключите своего бота, чтобы название и аватар были вашими.')}</> : tr('Бот не подключён: уведомления в Telegram не отправляются.')}
        </Banner>
      )}

      <Card title={b.connected ? tr('Заменить токен') : tr('Подключить своего бота')}>
        <ol className="mb-4 list-decimal space-y-1 pl-5 text-sm text-gray-700">
          <li>{tr('Откройте')} <a className="text-brand-600" href="https://t.me/BotFather" target="_blank" rel="noreferrer">@BotFather</a> {tr('в Telegram и отправьте')} <code>/newbot</code>.</li>
          <li>{tr('Укажите название (например, «')}{'Bilim Plus'}{tr('») и имя бота, оканчивающееся на')} <code>bot</code>.</li>
          <li>{tr('Скопируйте токен из ответа BotFather и вставьте ниже.')}</li>
        </ol>
        <div className="flex gap-2">
          <Input className="flex-1 font-mono" placeholder="123456789:AA…" value={token} onChange={(e) => setToken(e.target.value)} autoComplete="off" />
          <Button variant="primary" disabled={!token.trim()} loading={save.isPending && !!token} onClick={() => save.mutate({ token })}>{tr('Подключить')}</Button>
        </div>
        {error && <div className="mt-2 text-sm text-red-700">{error}</div>}
        <p className="mt-2 text-xs text-muted">{tr('Токен хранится на сервере и не показывается после сохранения. Один бот можно подключить только к одной школе.')}</p>
      </Card>

      {b.connected && (
        <>
          <Card title={tr('Приветствие')}>
            <Field hint={tr('Отправляется на /start и любое сообщение боту. Пусто — стандартный текст с инструкцией по входу.')}>
              <Textarea rows={4} value={welcome ?? b.welcome_text ?? ''} onChange={(e) => setWelcome(e.target.value)} placeholder={tr('Здравствуйте! Это бот учебного центра…')} />
            </Field>
            <Button className="mt-3" disabled={welcome === null} onClick={() => save.mutate({ welcome_text: welcome })}>{tr('Сохранить приветствие')}</Button>
          </Card>
          <Card title={tr('Уведомления родителям')}>
            <div className="space-y-3">
              {Object.entries(notifyLabels).map(([k, l]) => (
                <Checkbox key={k} checked={b.notify[k]} label={l} onChange={(v) => save.mutate({ notify: { ...b.notify, [k]: v } })} />
              ))}
            </div>
          </Card>
        </>
      )}

      <MiniAppTests onSaved={() => setToast(tr('Сохранено'))} />

      <Card title={tr('Личный кабинет')}>
        <div className="space-y-2 text-sm">
          <p>{tr('Ученики и родители входят по своему ИИН из карточки (пароль при первом входе —')} <b>12345</b>{tr('). Преподаватели — по ссылке-приглашению из раздела «Преподаватели».')}</p>
          <div className="flex gap-2">
            <Input readOnly value={b.miniapp_url} className="flex-1 font-mono text-xs" />
            <Button onClick={() => { navigator.clipboard?.writeText(b.miniapp_url); setToast(tr('Ссылка скопирована')) }}>{tr('Копировать')}</Button>
          </div>
          <p className="text-xs text-muted">{tr('Ссылка открывает кабинет в браузере; в Telegram кабинет открывается кнопкой «Кабинет» в боте.')}</p>
        </div>
      </Card>
      {toast && <Toast text={toast} onDone={() => setToast('')} />}
    </div>
  )
}

// Какие тесты преподаватель заполняет при отметке урока в Telegram Mini App и какие из них обязательные.
function MiniAppTests({ onSaved }: { onSaved: () => void }) {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['tests-config', 'admin', false], queryFn: () => api.get<TestConfig>('/tests/config') })
  const m = useMutation({
    mutationFn: ({ id, mode }: { id: string; mode: LessonMode }) => api.patch(`/test-types/${id}`, { lesson_mode: mode }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['tests-config'] }); onSaved() },
  })
  const types = q.data?.types.filter((t) => !t.archived) ?? []
  return (
    <Card title={tr('Тесты на уроке')} action={<Link to="/settings/testing" className="text-sm text-brand-600">{tr('Настроить тесты')}</Link>}>
      {!q.data ? <Spinner /> : types.length === 0 ? (
        <p className="text-sm text-muted">{tr('Типы тестов ещё не созданы. Добавьте их в разделе «Тестирование» — затем здесь можно выбрать, какие из них преподаватели проводят из Telegram.')}</p>
      ) : (
        <>
          <p className="mb-3 text-sm text-muted">{tr('Тесты «на уроке» преподаватель заполняет на экране отметки посещаемости в Mini App. Без обязательных занятие не закроется. Результаты приходят родителям вместе с темой и ДЗ.')}</p>
          <div className="space-y-3">
            {types.map((t) => (
              <div key={t.id} className="flex items-center justify-between gap-3 text-sm">
                <span>{t.name} <span className="text-muted">· {t.skills.length} {plural(t.skills.length, tr('навык'), tr('навыка'), tr('навыков'))}</span></span>
                <LessonModeSelect value={t.lesson_mode} disabled={m.isPending} onChange={(v) => m.mutate({ id: t.id, mode: v })} />
              </div>
            ))}
          </div>
        </>
      )}
    </Card>
  )
}
