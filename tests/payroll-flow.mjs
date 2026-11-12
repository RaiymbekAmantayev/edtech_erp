// Сценарный тест «Зарплата преподавателей и замены». Запуск: node tests/payroll-flow.mjs при запущенном API на свежих демо-данных
// (migrate -reset -seed): закрывает прошедшее занятие преподавателя и ставит замену на будущее.
const B = 'http://localhost:8080/api/v1';
let ok = 0, fail = 0;
const check = (name, cond, extra = '') => { if (cond) { ok++; console.log('  ✔', name); } else { fail++; console.log('  ✘', name, extra); } };
async function login(email) {
  const r = await fetch(B + '/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email, password: 'Demo12345!' }) });
  return (await r.json()).data?.access_token;
}
const api = (token) => async (method, path, body) => {
  const r = await fetch(B + path, { method, headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token }, body: body ? JSON.stringify(body) : undefined });
  const ct = r.headers.get('content-type') ?? '';
  if (!ct.includes('json')) return { status: r.status, type: ct, body: null };
  const t = await r.text();
  return { status: r.status, type: ct, body: t ? JSON.parse(t) : null };
};
const A = api(await login('aigerim@bilimplus.kz'));
const T = api(await login('erlan@bilimplus.kz'));
const month = new Date().toISOString().slice(0, 7);

console.log('Начисления');
const teachers = (await A('GET', '/employees?teachers=1')).body.data;
const erlan = teachers.find((t) => t.full_name === 'Ерлан Абенов');
check('ставка «за занятие» 5 000 ₸ сохранена', (await A('PATCH', `/employees/${erlan.id}`, { pay_type: 'per_lesson', pay_rate_tiyn: 500000 })).status < 300);

const cab = (await T('GET', '/teacher/cabinet')).body.data;
const lesson = cab.to_mark[0];
check('есть прошедшее занятие для отметки', !!lesson);
const att = (await T('GET', `/lessons/${lesson.id}/attendance`)).body.data;
const items = att.roster.map((x, i) => ({ student_id: x.student_id ?? '', lead_id: x.lead_id ?? '', status: i === 0 ? 'absent' : x.is_trial ? 'trial' : 'present' }));
let r = await T('PUT', `/lessons/${lesson.id}/attendance`, { items, close: true });
check('занятие закрыто', r.status === 200, JSON.stringify(r.body));

const mine = (await T('GET', `/teacher/payroll?month=${month}`)).body.data;
const admin = (await A('GET', `/payroll?month=${month}&teacher_id=${erlan.id}`)).body.data;
const row = mine.lessons.find((l) => l.lesson_id === lesson.id);
check('за урок начислено 5 000 ₸', row && row.amount_tiyn === 500000 && row.formula.includes('5 000'), JSON.stringify(row));
check('отметки в начислении совпадают с уроком', row && row.absent === 1 && row.students_total === att.roster.length);
check('кабинет преподавателя и админка совпадают', JSON.stringify(mine.total) === JSON.stringify(admin.total) && JSON.stringify(mine.lessons) === JSON.stringify(admin.lessons));
check('преподаватель не видит общий отчёт → 403', (await T('GET', `/payroll?month=${month}`)).status === 403);

// правка отметок пересчитывает начисление, ставка остаётся зафиксированной
await A('PATCH', `/employees/${erlan.id}`, { pay_type: 'hourly', pay_rate_tiyn: 400000 });
r = await T('PUT', `/lessons/${lesson.id}/attendance`, { items: items.map((x) => ({ ...x, status: x.status === 'absent' ? 'present' : x.status })), close: false });
const after = (await T('GET', `/teacher/payroll?month=${month}`)).body.data.lessons.find((l) => l.lesson_id === lesson.id);
check('правка отметок: отметки обновились, ставка прежняя', after.absent === 0 && after.pay_type === 'per_lesson' && after.amount_tiyn === 500000, JSON.stringify(after));
r = await A('POST', `/payroll/recalc?month=${month}&teacher_id=${erlan.id}`);
const re = (await T('GET', `/teacher/payroll?month=${month}`)).body.data.lessons.find((l) => l.lesson_id === lesson.id);
check('пересчёт по новой ставке: почасовая 4 000 ₸/ч', r.status === 200 && re.pay_type === 'hourly' && re.amount_tiyn === Math.round(400000 * re.minutes / 60), JSON.stringify(re));

r = await A('GET', `/payroll/export?month=${month}`);
check('выгрузка зарплаты в Excel', r.status === 200 && r.type.includes('spreadsheetml'));
r = await A('GET', `/tests?format=xlsx&from=2000-01-01`);
check('выгрузка тестов в Excel', r.status === 200 && r.type.includes('spreadsheetml'));

console.log('Замена преподавателя');
const future = cab.schedule.find((l) => new Date(l.starts_at) > new Date(Date.now() + 3600e3));
const subs = (await A('GET', `/lessons/${future.id}/substitutes`)).body.data;
check('текущий преподаватель отмечен', subs.some((s) => s.current && s.id === erlan.id));
const free = subs.find((s) => s.free && !s.current);
const busy = subs.find((s) => !s.free && !s.current);
check('есть свободный преподаватель', !!free, JSON.stringify(subs.map((s) => [s.full_name, s.free, s.busy])));
if (busy) check('занятого поставить нельзя → 409', (await A('POST', `/lessons/${future.id}/substitute`, { teacher_id: busy.id })).status === 409);
r = await A('POST', `/lessons/${future.id}/substitute`, { teacher_id: free.id, reason: 'Болезнь' });
check('замена поставлена', r.status === 204, JSON.stringify(r.body));
const l2 = (await A('GET', `/lessons/${future.id}`)).body.data;
check('в занятии — новый преподаватель и «замена за»', l2.teacher_id === free.id && l2.substitute_for === 'Ерлан Абенов', JSON.stringify(l2));
check('занятие ушло из кабинета прежнего преподавателя', !(await T('GET', '/teacher/cabinet')).body.data.schedule.some((l) => l.id === future.id));
check('преподаватель не ставит замену → 403', (await T('POST', `/lessons/${future.id}/substitute`, { teacher_id: erlan.id })).status === 403);
r = await A('POST', `/lessons/${future.id}/substitute`, { teacher_id: erlan.id });
const l3 = (await A('GET', `/lessons/${future.id}`)).body.data;
check('возврат основному преподавателю снимает замену', r.status === 204 && l3.teacher_id === erlan.id && l3.substitute_for === null);

// замена на проведённом уроке переносит начисление
const subs2 = (await A('GET', `/lessons/${lesson.id}/substitutes`)).body.data.filter((s) => s.free && !s.current);
if (subs2.length) {
  await A('POST', `/lessons/${lesson.id}/substitute`, { teacher_id: subs2[0].id });
  const other = (await A('GET', `/payroll?month=${month}&teacher_id=${subs2[0].id}`)).body.data.lessons.find((l) => l.lesson_id === lesson.id);
  const gone = !(await T('GET', `/teacher/payroll?month=${month}`)).body.data.lessons.some((l) => l.lesson_id === lesson.id);
  check('начисление за проведённый урок перешло замене', other && other.substitute_for === 'Ерлан Абенов' && gone, JSON.stringify(other));
}

console.log(`\n${ok} ✔  ${fail} ✘`);
process.exit(fail ? 1 : 0);
