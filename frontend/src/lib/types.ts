export type CFOption = { id: string; label: Record<string, string>; color: string | null; sort_order: number; archived_at: string | null }
export type CFSettings = {
  required?: '' | 'always' | 'on_status'
  required_status?: string
  min?: number
  max?: number
  precision?: number
  mask?: string
  placeholder?: string
  hint?: string
  default?: unknown
  branch_ids?: string[]
  course_ids?: string[]
  roles_view?: string[]
  roles_edit?: string[]
  show_in_table?: boolean
  show_in_create?: boolean
  show_in_teacher?: boolean
  show_in_parent?: boolean
  show_in_web_form?: boolean
}
export type CField = {
  id: string
  entity_type: string
  key: string
  field_type: string
  label: Record<string, string>
  block_id: string | null
  sort_order: number
  settings: CFSettings
  is_sensitive: boolean
  is_unique: boolean
  archived_at: string | null
  options: CFOption[]
}
export type CFBlock = { id: string; entity_type: string; name: Record<string, string>; sort_order: number }

export type Subscription = { id: string; type: string; name: string; lessons_total: number | null; lessons_used: number; starts_on: string; ends_on: string | null; status: string; price_tiyn: number; frozen_until?: string | null }
export type StudentRow = {
  id: string
  number: number
  full_name: string
  phone: string | null
  email: string | null
  birth_date: string | null
  status: string
  branch_id: string
  iin: string | null
  source: string | null
  balance_tiyn?: number | null
  groups: { id: string; name: string; course: string; course_id: string; type: string }[]
  subscription: Subscription | null
  custom_fields: Record<string, unknown>
  created_at: string
}

export type Lesson = {
  id: string
  group_id: string
  group_name: string
  group_type: string
  course_name: string
  branch_id: string
  teacher_id: string | null
  teacher_name: string | null
  classroom_id: string | null
  classroom_name: string | null
  capacity: number | null
  starts_at: string
  ends_at: string
  type: string
  status: string
  state: string
  topic: string | null
  schedule_id: string | null
  students: number
  trials: number
  marked: number
  conflict: boolean
}

export type Group = {
  id: string
  branch_id: string
  branch_name: string
  course_id: string
  course_name: string
  name: string
  type: string
  teacher_id: string | null
  teacher_name: string | null
  classroom_id: string | null
  classroom_name: string | null
  start_date: string | null
  end_date: string | null
  capacity: number | null
  status: string
  students: number
  waitlist: number
  schedule: { id: string; weekdays: number[]; start_time: string; end_time: string; valid_from: string; valid_to: string | null }[]
  custom_fields: Record<string, unknown>
}

export type Course = { id: string; name: string; description: string | null; category: string | null; duration_minutes: number; lessons_count: number; price_tiyn: number; status: string; groups: number; students: number }
export type Employee = { id: string; user_id: string | null; full_name: string; phone: string | null; email: string | null; position: string | null; is_teacher: boolean; subjects: string[]; branch_ids: string[]; status: string; pay_type: string; pay_rate_tiyn?: number; groups_count: number; week_lessons: number; roles: string[] }
export type Classroom = { id: string; branch_id: string; name: string; capacity: number | null; is_online: boolean; equipment: string | null; status: string }
