// Модуль «Тестирование»: типы тестов, навыки, оценки и результаты.
export type TestSkill = { id: string; name: string; archived: boolean }
export type TestGrade = { id: string; name: string; color: string | null; min_percent: number | null; archived: boolean }
export type LessonMode = 'off' | 'optional' | 'required'
export type TestType = {
  id: string
  name: string
  description: string | null
  scoring: 'score' | 'grade'
  lesson_mode: LessonMode
  course_ids: string[]
  archived: boolean
  skills: { skill_id: string; name: string; max_score: number }[]
  results: number
}
export type TestConfig = { types: TestType[]; skills: TestSkill[]; grades: TestGrade[] }
export type GradeRef = { id: string; name: string; color: string | null }
export type TestResult = {
  id: string
  taken_on: string
  student_id: string
  student: string
  test_type_id: string
  test_type: string
  scoring: 'score' | 'grade'
  group: string | null
  lesson_id: string | null
  teacher: string | null
  total_score: number | null
  max_score: number | null
  percent: number | null
  grade: GradeRef | null
  comment: string | null
  created_by: string | null
  created_at: string
  scores: { skill_id: string; skill: string; score: number | null; max_score: number | null; grade: GradeRef | null }[]
}

export const scoringLabel = { score: 'Баллы по навыкам', grade: 'Оценка по навыкам' }
export const lessonModeLabel: Record<LessonMode, string> = { off: 'Не на уроке', optional: 'На уроке, необязательный', required: 'На уроке, обязательный' }

export const num = (n: number | null | undefined) => (n == null ? '—' : Number.isInteger(n) ? String(n) : n.toFixed(1).replace('.', ','))

// Оценка, которая будет выставлена автоматически по проценту (как на сервере).
export function autoGrade(grades: TestGrade[], percent: number | null): TestGrade | null {
  if (percent == null) return null
  return [...grades].filter((g) => g.min_percent != null && !g.archived).sort((a, b) => b.min_percent! - a.min_percent!).find((g) => percent >= g.min_percent!) ?? null
}

// Сводка введённых баллов одного ученика.
export function summarize(type: TestType, scores: Record<string, string>) {
  let total = 0, max = 0, filled = 0
  for (const s of type.skills) {
    const v = scores[s.skill_id]
    if (v == null || v === '') continue
    const n = Number(v.replace(',', '.'))
    if (Number.isNaN(n)) continue
    total += n; max += s.max_score; filled++
  }
  return { total, max, filled, percent: max ? Math.round((total / max) * 10000) / 100 : null }
}

export const gradeStyle = (c: string | null | undefined) => (c ? { backgroundColor: c + '22', color: c } : undefined)

