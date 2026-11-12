import { useState } from 'react'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import type { CField } from '../lib/types'
import { Banner, Button, Field, Modal, Select } from '../components/ui'
import { tr } from '../lib/i18n'

type Preview = { headers: string[]; rows: string[][]; total: number; mapping: Record<string, string>; targets: { key: string; label: string }[] }
type Report = { created: number; updated: number; skipped: number; duplicates: number; errors: { row: number; field: string; code: string }[]; dry_run: boolean }

// Импорт учеников из XLSX/CSV (ТЗ 24.2): загрузка → сопоставление колонок → пробный прогон → импорт → отчёт.
export function ImportModal({ onClose, fields }: { onClose: () => void; fields: CField[] }) {
  const { me, branchId } = useAuth()
  const [file, setFile] = useState<File | null>(null)
  const [preview, setPreview] = useState<Preview | null>(null)
  const [mapping, setMapping] = useState<Record<string, string>>({})
  const [branch, setBranch] = useState(branchId || me?.branches?.[0]?.id || '')
  const [dupMode, setDupMode] = useState('skip')
  const [report, setReport] = useState<Report | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const form = (mode: string) => {
    const fd = new FormData()
    fd.append('file', file!)
    fd.append('mode', mode)
    fd.append('mapping', JSON.stringify(mapping))
    fd.append('branch_id', branch)
    fd.append('duplicates', dupMode)
    return fd
  }
  const run = async (mode: string) => {
    setBusy(true)
    setError('')
    try {
      if (mode === 'preview') {
        const p = await api.upload<Preview>('/students/import', form('preview'))
        setPreview(p)
        setMapping(p.mapping)
      } else setReport(await api.upload<Report>('/students/import', form(mode)))
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const codeText: Record<string, string> = { REQUIRED: tr('не заполнено'), INVALID_FORMAT: tr('неверный формат'), INVALID_IIN: tr('неверный ИИН'), INVALID_OPTION: tr('нет такого варианта'), ROW_FAILED: tr('ошибка строки'), INVALID_DATE: tr('неверная дата'), INVALID_NUMBER: tr('не число'), TOO_LARGE: tr('больше допустимого'), NOT_UNIQUE: tr('не уникально') }
  const downloadErrors = () => {
    if (!report || !preview) return
    const csv = [tr('Строка;Поле;Ошибка'), ...report.errors.map((e) => `${e.row};${e.field};${codeText[e.code] ?? e.code}`)].join('\n')
    const a = document.createElement('a')
    a.href = URL.createObjectURL(new Blob(['﻿' + csv], { type: 'text/csv' }))
    a.download = 'import-errors.csv'
    a.click()
  }

  return (
    <Modal open wide="xl" title={tr('Импорт учеников из Excel / CSV')} onClose={onClose}
      footer={
        !preview ? <><Button onClick={onClose}>{tr('Отмена')}</Button><Button variant="primary" disabled={!file} loading={busy} onClick={() => run('preview')}>{tr('Загрузить')}</Button></> :
        report && !report.dry_run ? <Button variant="primary" onClick={onClose}>{tr('Готово')}</Button> :
        <><Button onClick={() => { setPreview(null); setReport(null) }}>{tr('Другой файл')}</Button><Button loading={busy} onClick={() => run('dry_run')}>{tr('Пробный прогон')}</Button>
          <Button variant="primary" loading={busy} disabled={!Object.values(mapping).includes('full_name')} onClick={() => run('commit')}>{tr('Импортировать')} {preview.total}</Button></>
      }>
      {!preview && (
        <div>
          <label className="flex cursor-pointer flex-col items-center justify-center rounded-xl border-2 border-dashed border-line px-6 py-12 text-center hover:bg-gray-50">
            <input type="file" accept=".xlsx,.csv" className="hidden" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
            <div className="text-[15px] font-semibold">{file ? file.name : tr('Выберите файл XLSX или CSV')}</div>
            <div className="mt-1 text-sm text-muted">{tr('Первая строка — заголовки колонок. Поддерживаются выгрузки других CRM и Excel.')}</div>
          </label>
          <p className="mt-3 text-xs text-muted">{tr('Колонки сопоставляются автоматически по названию, в том числе с пользовательскими полями (')}{fields.length}{tr('). Значения списков сопоставляются по названию варианта.')}</p>
        </div>
      )}
      {preview && (
        <div className="space-y-5">
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={tr('Филиал')}><Select value={branch} onChange={(e) => setBranch(e.target.value)}>{me?.branches?.map((b) => <option key={b.id} value={b.id}>{b.name}</option>)}</Select></Field>
            <Field label={tr('Если найден дубликат по телефону')}>
              <Select value={dupMode} onChange={(e) => setDupMode(e.target.value)}><option value="skip">{tr('Пропустить строку')}</option><option value="update">{tr('Обновить существующего')}</option><option value="create">{tr('Всё равно создать')}</option></Select>
            </Field>
          </div>
          <div>
            <div className="mb-2 text-sm font-semibold">{tr('Сопоставление колонок ·')} {preview.total} {tr('строк')}</div>
            <div className="overflow-x-auto rounded-xl border border-line">
              <table className="text-sm">
                <thead>
                  <tr className="border-b border-line bg-gray-50">
                    {preview.headers.map((h) => (
                      <th key={h} className="min-w-44 p-2 text-left align-top">
                        <div className="mb-1.5 truncate text-xs font-semibold text-muted">{h}</div>
                        <Select className="!h-8 text-xs" value={mapping[h] ?? ''} onChange={(e) => setMapping({ ...mapping, [h]: e.target.value })}>
                          <option value="">{tr('— не импортировать —')}</option>
                          {preview.targets.map((t) => <option key={t.key} value={t.key}>{t.label}</option>)}
                        </Select>
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody className="divide-y divide-line">
                  {preview.rows.slice(0, 6).map((r, i) => <tr key={i}>{preview.headers.map((_, j) => <td key={j} className="max-w-52 truncate p-2 text-[13px]">{r[j]}</td>)}</tr>)}
                </tbody>
              </table>
            </div>
          </div>
          {report && (
            <Banner tone={report.errors.length ? 'yellow' : 'teal'} action={report.errors.length > 0 && <Button size="sm" onClick={downloadErrors}>{tr('Скачать ошибки')}</Button>}>
              <div className="font-semibold">{report.dry_run ? tr('Пробный прогон (ничего не сохранено)') : tr('Импорт завершён')}</div>
              <div>{tr('Будет создано/создано:')} {report.created} {tr('· обновлено:')} {report.updated} {tr('· пропущено:')} {report.skipped} {tr('· дубликатов:')} {report.duplicates} {tr('· ошибок:')} {report.errors.length}</div>
              {report.errors.slice(0, 5).map((e, i) => <div key={i} className="text-xs">{tr('Строка')} {e.row}: {e.field.replace('custom_fields.', '')} — {codeText[e.code] ?? e.code}</div>)}
            </Banner>
          )}
        </div>
      )}
      {error && <div className="mt-3 text-sm text-red-700">{error}</div>}
    </Modal>
  )
}
