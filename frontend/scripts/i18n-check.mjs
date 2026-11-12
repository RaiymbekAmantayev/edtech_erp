// Проверка казахского словаря: все тексты из tr('…') в src/ должны быть в src/lib/i18n.kk.ts.
// Запуск из папки frontend: node scripts/i18n-check.mjs   (код выхода 1, если есть строки без перевода)
// Также предупреждает о русском тексте вне tr() в JSX (грубая проверка по строкам с кириллицей между тегами).
import fs from 'node:fs'
import path from 'node:path'

const src = path.resolve(import.meta.dirname, '..', 'src')
const unq = (s) => s.replace(/\\n/g, '\n').replace(/\\'/g, "'").replace(/\\\\/g, '\\')
const files = (d) => fs.readdirSync(d, { withFileTypes: true }).flatMap((e) => (e.isDirectory() ? files(path.join(d, e.name)) : /\.tsx?$/.test(e.name) ? [path.join(d, e.name)] : []))

const dict = new Set()
for (const m of fs.readFileSync(path.join(src, 'lib', 'i18n.kk.ts'), 'utf8').matchAll(/^\s*'((?:[^'\\]|\\.)*)':/gm)) dict.add(unq(m[1]))

const missing = new Map()
const loose = []
for (const f of files(src)) {
  if (f.endsWith('i18n.kk.ts') || f.endsWith(path.join('lib', 'i18n.ts'))) continue
  const text = fs.readFileSync(f, 'utf8')
  for (const m of text.matchAll(/\btr\('((?:[^'\\]|\\.)*)'/g)) {
    const key = unq(m[1])
    if (!dict.has(key)) missing.set(key, path.relative(src, f))
  }
  text.split('\n').forEach((line, i) => {
    if (/>[^<>{}]*[А-Яа-яЁё][^<>{}]*</.test(line) && !line.trim().startsWith('//')) loose.push(`${path.relative(src, f)}:${i + 1}`)
  })
}

for (const [k, f] of missing) console.log(`нет перевода  ${f}  ${JSON.stringify(k)}`)
for (const l of loose) console.log(`текст вне tr()  ${l}`)
console.log(`\nв словаре: ${dict.size}, без перевода: ${missing.size}, текст вне tr(): ${loose.length}`)
process.exit(missing.size ? 1 : 0)
