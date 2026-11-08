# EMP — платформа для учебных центров (MVP)

Реализация MVP по ТЗ v1.1 (раздел 32): модульный монолит на Go + PostgreSQL с изоляцией организаций через RLS,
веб-интерфейс на React по макетам «EMP — дизайн интерфейса».

## Быстрый старт (Windows)

Нужно: PostgreSQL 15+ (локально, пользователь `postgres`), Node.js 20+, Go 1.27+ (если не в PATH — ожидается в `%USERPROFILE%\sdk\go`).

```powershell
.\start.ps1 -Setup     # первый запуск: роли emp_owner/emp_app, база emp, миграции, демо-данные
.\start.ps1            # обычный запуск
.\start.ps1 -Reset     # пересоздать базу с демо-данными
```

Откройте http://localhost:5173. Пароль всех демо-пользователей — `Demo12345!`:

| Email | Роль |
|---|---|
| aigerim@bilimplus.kz | Владелец центра «Bilim Plus» |
| erlan@bilimplus.kz | Преподаватель (видит только свои занятия, мобильная отметка) |
| madi@bilimplus.kz | Менеджер продаж |
| buh@bilimplus.kz | Бухгалтер |
| admin@emp.kz | Super Admin платформы (только агрегаты, без ПД учеников) |

Настройки — в `backend/.env` (не коммитится): строки подключения, `JWT_SECRET`, `TELEGRAM_BOT_TOKEN`.
Без токена бота уведомления не отправляются, а пишутся в `notification_logs` со статусом `skipped`.

## Структура

```
backend/
  cmd/api, cmd/worker, cmd/migrate     точки входа
  internal/platform/                    config, database (RLS-транзакции, аудит, outbox), auth, httpx, middleware, validate
  internal/modules/                     identity, organization, branch, customfield, student, parent, employee, group,
                                        schedule, attendance, finance, lead, reporting, notification, platform
  internal/seed/                        демо-данные по макетам
  migrations/                           SQL-миграции (встраиваются в бинарник)
frontend/src/                           React + TypeScript + Tailwind + TanStack Query
tests/api-flow.mjs                      сценарный тест API по критериям готовности MVP
```

## Ключевые решения

- **Изоляция организаций.** API работает под ролью `emp_app` (без `BYPASSRLS`, не владелец таблиц). Каждый запрос — транзакция
  с `set_config('app.tenant_id', …, true)`; без контекста запросы возвращают 0 строк. Составные FK `(tenant_id, id)`
  не дают сослаться на запись другой организации. Миграции и worker — под `emp_owner`.
- **Деньги** — `BIGINT` в тиын, без float. Баланс считается из неизменяемых проводок (`ledger_entries`), `student_balances` — кэш.
  Исправления — только корректирующими операциями; `emp_app` не может удалять платежи и проводки.
- **Пользовательские поля** — определения в `custom_field_definitions`, значения в `custom_fields JSONB` + GIN-индекс.
  Серверная валидация по типу, обязательность по статусу, видимость/редактирование по ролям, маскирование чувствительных,
  фильтры `?cf.key[gte]=…`, импорт/экспорт, ключи для API.
- **Посещаемость → списания.** Закрытие занятия списывает его с абонемента по правилам статусов; изменение отметки задним
  числом даёт корректирующую запись, а не перезапись. Заморозка продлевает абонемент.
- **Расписание** — серии с генерацией на 8 недель вперёд (worker продлевает), праздники РК и нерабочие дни центра →
  «Требует решения», проверка конфликтов преподавателя/кабинета/группы/вместимости, правка «это / последующие / вся серия».
- **Outbox** — уведомления (пропуск, опоздание, отмена, абонемент заканчивается, оплата) пишутся в `outbox_events`
  в той же транзакции; worker доставляет в Telegram с учётом «тихих часов» и журналом доставки.
- **Сессии** — Argon2id, JWT 15 мин, refresh-токен в httpOnly-cookie с ротацией и отзывом всех сессий при повторном использовании.

## Тесты

```powershell
cd backend; go test ./...             # валидация, пользовательские поля, интеграционный тест RLS-изоляции (нужна локальная БД)
node tests/api-flow.mjs               # 32 сценарные проверки по API (запущенный API; меняет демо-данные)
```

## Личный кабинет и Telegram-боты школ

- **Бот школы** — «Настройки → Telegram-бот»: токен из @BotFather, приветствие, типы уведомлений. Кнопка меню бота
  «Кабинет» открывает Mini App `/app?t=<id школы>`. Школы без своего бота используют общий бот из `TELEGRAM_BOT_TOKEN`.
  Worker опрашивает всех ботов (`getUpdates`); chat_id родителя запоминается вместе с ботом, через которого он привязан.
- **Ученик и родитель** входят по своему ИИН из карточки (у родителя — поле «ИИН»), пароль при первом входе `12345`,
  меняется в профиле. Учётная запись (`portal_accounts`) создаётся при первом входе; токен кабинета выпускается с
  отдельным issuer и не принимается API сотрудников. Вход из Telegram привязывает аккаунт, у родителя — и уведомления.
- **Преподаватель** получает одноразовую ссылку-приглашение (карточка сотрудника → «Дать доступ»), после неё входит в
  Mini App сессией сотрудника с ролью «Преподаватель»: расписание, неотмеченные и проведённые уроки, статистика, отметка
  посещаемости (`/app/lesson/:id`).

## Деплой (https://erp.bilim-shynyna-42-qadam.kz)

GitHub Actions ([.github/workflows/ci-cd.yml](.github/workflows/ci-cd.yml)): на PR — `go vet`, `go test` (с PostgreSQL 16),
lint и сборка фронтенда; на push в `master` — дополнительно выкладка на сервер через SSH пользователем `emp`.

Сервер — AlmaLinux 8, общий с PHP-сайтом `bilim-shynyna-42-qadam.kz` (Apache + MariaDB), который не затрагивается:

- PostgreSQL 16 на `localhost:5432`; API на `127.0.0.1:8080` и worker — systemd-сервисы `emp-api`, `emp-worker`.
- Apache-vhost `/etc/httpd/conf.d/erp*.conf`: статика из `/var/www/erp/current`, `/api` и `/health` проксируются в API.
  Сертификат Let's Encrypt продлевается штатным `certbot-renew.timer`.
- Релизы — `/opt/emp/releases/<sha>`, `/opt/emp/current` → активный; секреты — `/opt/emp/shared/.env` (только на сервере).
- [deploy/deploy.sh](deploy/deploy.sh) применяет миграции, переключает симлинки, перезапускает сервисы и при неудачной
  проверке `/health` возвращает предыдущий релиз. Первичная настройка сервера — [deploy/server-setup.sh](deploy/server-setup.sh).

Секреты репозитория: `DEPLOY_SSH_KEY` (приватный ключ для `emp`), `DEPLOY_KNOWN_HOSTS`, `DEPLOY_HOST`.
Демо-данные на production не загружаются; при необходимости: `ssh emp@сервер 'cd /opt/emp/shared && ../current/migrate -seed'`.

## Что не входит в MVP (этапы roadmap)

Канбан воронки и задачи менеджеров, документы и шаблоны договоров, WhatsApp/SMS и массовые рассылки, онлайн-оплата и
фискализация, Public API-ключи и вебхуки, amoCRM, портал родителя/ученика, оценки и ДЗ, зарплата, impersonation поддержки.
Отличия от стека ТЗ: очередь — Postgres outbox + worker вместо Asynq/Redis; SQL написан вручную (без sqlc);
партиционирование журналов и Prometheus/Loki — на этапе Production Hardening.
