// Сценарный тест «Настройки зарплаты и показ начислений преподавателю». Запуск: node tests/payroll-settings-flow.mjs при запущенном API.
// Работает на текущих данных: в конце возвращает настройки школы и условия оплаты преподавателя как были.
const B = 'http://localhost:8080/api/v1';
let ok = 0, fail = 0;
const check = (name, cond, extra = '') => { if (cond) { ok++; console.log('  ✔', name); } else { fail++; console.log('  ✘', name, extra); } };
async function login(email) {
  const r = await fetch(B + '/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email, password: 'Demo12345!' }) });
  return (await r.json()).data?.access_token;
}
const api = (token) => async (method, path, body) => {
  const r = await fetch(B + path, { method, headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token }, body: body ? JSON.stringify(body) : undefined });
  const t = await r.text();
  return { status: r.status, body: t ? JSON.parse(t) : null };
};
const A = api(await login('aigerim@bilimplus.kz'));
const T = api(await login('erlan@bilimplus.kz'));

const original = (await A('GET', '/settings/payroll')).body.data;
const erlan = (await A('GET', '/employees?teachers=1')).body.data.find((t) => t.full_name === 'Ерлан Абенов');
const pay = { pay_type: erlan.pay_type, pay_rate_tiyn: erlan.pay_rate_tiyn, pay_percent: erlan.pay_percent ?? 0, payroll_visible: erlan.payroll_visible };
// месяц, в котором у преподавателя есть начисления (для проверки расчёта)
let month = new Date().toISOString().slice(0, 7);
for (let i = 0; i < 12; i++) {
  const d = new Date(); d.setUTCDate(1); d.setUTCMonth(d.getUTCMonth() - i);
  const m = d.toISOString().slice(0, 7);
  if ((await A('GET', `/payroll?month=${m}&teacher_id=${erlan.id}`)).body.data.lessons.length) { month = m; break; }
}

try {
  console.log('Настройки по умолчанию и права');
  check('настройки читаются', original && typeof original.show_to_teachers === 'boolean' && original.substitute, JSON.stringify(original));
  check('преподаватель не видит настройки → 403', (await T('GET', '/settings/payroll')).status === 403);
  let r = await A('PUT', '/settings/payroll', { ...original, substitute: 'bonus' });
  check('неверный режим замены отклоняется', r.status >= 400 && r.status < 500, JSON.stringify(r.body));
  r = await A('PUT', '/settings/payroll', { ...original, min_students: -1 });
  check('отрицательный минимум учеников отклоняется', r.status >= 400 && r.status < 500);

  console.log('Показ начислений в кабинете преподавателя');
  await A('PATCH', `/employees/${erlan.id}`, { payroll_visible: 'school' });
  await A('PUT', '/settings/payroll', { ...original, show_to_teachers: true });
  check('школа показывает → кабинет: payroll_visible = true', (await T('GET', '/teacher/cabinet')).body.data.payroll_visible === true);
  check('школа показывает → /teacher/payroll 200', (await T('GET', `/teacher/payroll?month=${month}`)).status === 200);
  await A('PUT', '/settings/payroll', { ...original, show_to_teachers: false });
  check('школа скрыла → кабинет: payroll_visible = false', (await T('GET', '/teacher/cabinet')).body.data.payroll_visible === false);
  r = await T('GET', `/teacher/payroll?month=${month}`);
  check('школа скрыла → /teacher/payroll 403 PAYROLL_HIDDEN', r.status === 403 && JSON.stringify(r.body).includes('PAYROLL_HIDDEN'), JSON.stringify(r.body));
  await A('PATCH', `/employees/${erlan.id}`, { payroll_visible: 'show' });
  check('исключение в карточке «Показывать» сильнее настройки школы', (await T('GET', `/teacher/payroll?month=${month}`)).status === 200);
  check('карточка отдаёт payroll_visible = show', (await A('GET', `/employees/${erlan.id}`)).body.data.employee.payroll_visible === 'show');
  await A('PUT', '/settings/payroll', { ...original, show_to_teachers: true });
  await A('PATCH', `/employees/${erlan.id}`, { payroll_visible: 'hide' });
  check('исключение «Скрыть» при открытой школе → 403', (await T('GET', `/teacher/payroll?month=${month}`)).status === 403);
  r = await A('PATCH', `/employees/${erlan.id}`, { payroll_visible: 'maybe' });
  check('неверное значение показа отклоняется', r.status >= 400 && r.status < 500);

  console.log(`Расчёт (месяц ${month})`);
  const recalc = () => A('POST', `/payroll/recalc?month=${month}&teacher_id=${erlan.id}`);
  const lessons = async () => (await A('GET', `/payroll?month=${month}&teacher_id=${erlan.id}`)).body.data.lessons;
  await A('PUT', '/settings/payroll', { ...original, show_to_teachers: true, min_students: 0, substitute: 'own', substitute_value: 100 });
  await A('PATCH', `/employees/${erlan.id}`, { pay_type: 'mixed', pay_rate_tiyn: 500000, pay_percent: 1000 });
  check('карточка сохранила процент комбинированной модели', (await A('GET', `/employees/${erlan.id}`)).body.data.employee.pay_percent === 1000);
  await recalc();
  let ls = await lessons();
  const paid = ls.filter((l) => l.students_paid > 0 && !l.substitute_for);
  check('есть оплачиваемые занятия для проверки', paid.length > 0, `month ${month}`);
  check('комбинированная: ставка + 10% от стоимости урока',
    paid.every((l) => l.pay_type === 'mixed' && l.pay_percent === 1000 && l.amount_tiyn === 500000 + Math.round(l.revenue_tiyn * 0.1) && l.formula.includes('+ 10%')),
    JSON.stringify(paid[0]));

  await A('PUT', '/settings/payroll', { ...original, min_students: 100, below_min: 'min_amount', min_amount_tiyn: 200000, substitute: 'own', substitute_value: 100 });
  await recalc();
  ls = await lessons();
  check('меньше минимума учеников → минимальная оплата 2 000 ₸', ls.every((l) => l.amount_tiyn === 200000 && l.formula.includes('минимальная оплата')), JSON.stringify(ls[0]));
  await A('PUT', '/settings/payroll', { ...original, min_students: 100, below_min: 'zero', min_amount_tiyn: 0, substitute: 'own', substitute_value: 100 });
  await recalc();
  ls = await lessons();
  check('меньше минимума учеников → урок не оплачивается', ls.every((l) => l.amount_tiyn === 0 && l.formula.includes('не оплачивается')), JSON.stringify(ls[0]));

  const xlsx = await fetch(`${B}/payroll/export?month=${month}`, { headers: { Authorization: 'Bearer ' + (await login('aigerim@bilimplus.kz')) } });
  check('выгрузка в Excel работает', xlsx.status === 200 && (xlsx.headers.get('content-type') ?? '').includes('spreadsheetml'));
} finally {
  // вернуть как было
  await A('PUT', '/settings/payroll', original);
  await A('PATCH', `/employees/${erlan.id}`, pay);
  await A('POST', `/payroll/recalc?month=${month}&teacher_id=${erlan.id}`);
}

console.log(`\n${ok} passed, ${fail} failed`);
process.exit(fail ? 1 : 0);
