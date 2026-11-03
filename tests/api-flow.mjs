// Сценарный тест API (MVP-критерии раздела 32.3). Запуск: node tests/api-flow.mjs при запущенном API.
// Внимание: создаёт тестовые записи и закрывает сегодняшнее занятие 11А — после него можно пересоздать демо-данные (migrate -reset -seed).
const B = 'http://localhost:8080/api/v1';
let ok = 0, fail = 0;
const check = (name, cond, extra = '') => { if (cond) { ok++; console.log('  ✔', name); } else { fail++; console.log('  ✘', name, extra); } };

async function login(email) {
  const r = await fetch(B + '/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email, password: 'Demo12345!' }) });
  const j = await r.json();
  return { token: j.data?.access_token, cookie: r.headers.get('set-cookie') };
}
function api(token) {
  return async (method, path, body, headers = {}) => {
    const r = await fetch(B + path, { method, headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token, ...headers }, body: body ? JSON.stringify(body) : undefined });
    const t = await r.text();
    let j = null; try { j = t ? JSON.parse(t) : null; } catch {}
    return { status: r.status, body: j };
  };
}

for (let i = 0; i < 20; i++) { try { await fetch('http://localhost:8080/health'); break; } catch { await new Promise(r => setTimeout(r, 300)); } }

const owner = await login('aigerim@bilimplus.kz');
const A = api(owner.token);
console.log('Владелец');
const groups = (await A('GET', '/groups')).body.data;
const g11a = groups.find(g => g.name === 'ЕНТ Математика 11А');
const branchId = g11a.branch_id;
const fields = (await A('GET', '/custom-fields?entity=student')).body.data;
const grade11 = fields.find(f => f.key === 'grade').options.find(o => o.label.ru === '11').id;

// валидация пользовательских полей
let r = await A('POST', '/students', { full_name: 'Тест Тестов', branch_id: branchId, status: 'active', group_id: g11a.id, phone: '8 701 555 44 33', custom_fields: { target_score: 200 } });
check('cf: значение вне диапазона и обязательные поля → 422', r.status === 422 && r.body.error.fields.some(f => f.field === 'custom_fields.target_score' && f.code === 'TOO_LARGE'), JSON.stringify(r.body));
r = await A('POST', '/students', { full_name: 'Тест', branch_id: branchId, custom_fields: { unknown_key: 1 } });
check('cf: неизвестный ключ → CUSTOM_FIELD_UNKNOWN', r.body?.error?.code === 'CUSTOM_FIELD_UNKNOWN');
r = await A('POST', '/students', { full_name: 'Тест', branch_id: branchId, iin: '123456789012' });
check('ИИН с неверной контрольной цифрой → INVALID_IIN', r.body?.error?.fields?.[0]?.code === 'INVALID_IIN');
r = await A('POST', '/students', { full_name: 'Тест Тестов', branch_id: branchId, status: 'active', group_id: g11a.id, phone: '8 701 555 44 33',
  custom_fields: { grade: grade11, target_score: 121, school: 'Школа №1' }, parents: [{ full_name: 'Тестова Мама', phone: '+7 (707) 111-22-33', relation: 'mother', is_payer: true, consent: true }] });
check('создание ученика с родителем и группой', r.status === 201, JSON.stringify(r.body));
const sid = r.body.data.id;
let card = (await A('GET', '/students/' + sid)).body.data;
check('телефон нормализован в E.164', card.student.phone === '+77015554433', card.student.phone);
check('родитель привязан', card.parents.length === 1 && card.parents[0].phone === '+77071112233');
check('ученик в группе', card.groups[0]?.id === g11a.id);

// фильтр по пользовательскому полю
r = await A('GET', '/students?cf.grade=11&cf.target_score[gte]=121&cf.target_score[lte]=121');
check('фильтр по cf (список + диапазон числа)', r.body.data.some(s => s.id === sid));

// оптимистичная блокировка
const v = card.details.version;
r = await A('PATCH', '/students/' + sid, { version: v, custom_fields: { target_score: 125 } });
check('PATCH с актуальной версией', r.status === 200 && r.body.data.version === v + 1);
r = await A('PATCH', '/students/' + sid, { version: v, full_name: 'Другое имя' });
check('PATCH со старой версией → VERSION_CONFLICT', r.status === 409 && r.body.error.code === 'VERSION_CONFLICT');
const hist = (await A('GET', `/students/${sid}/history`)).body.data;
check('история изменений: cf.target_score 121 → 125', hist.some(h => h.old?.['cf.target_score'] === 121 && h.new?.['cf.target_score'] === 125));

// абонемент, оплата, idempotency
r = await A('POST', '/subscriptions', { student_id: sid, course_id: null, type: 'lessons', lessons_total: 12, price_tiyn: 7200000, starts_on: '2026-11-01' });
check('продажа абонемента', r.status === 201, JSON.stringify(r.body));
const key = 'test-' + Date.now();
r = await A('POST', '/payments', { student_id: sid, amount_tiyn: 5400000, method: 'kaspi' }, { 'Idempotency-Key': key });
const r2 = await A('POST', '/payments', { student_id: sid, amount_tiyn: 5400000, method: 'kaspi' }, { 'Idempotency-Key': key });
check('повтор платежа с тем же Idempotency-Key не создаёт дубль', r.status === 201 && r2.body.data.id === r.body.data.id);
check('баланс = 54 000 − 72 000 = −18 000 ₸', r.body.data.balance_tiyn === -1800000, r.body.data.balance_tiyn);
r = await A('POST', '/payments', { student_id: sid, amount_tiyn: 0, method: 'cash' });
check('платёж 0 ₸ отклонён', r.status === 422);

// посещаемость сегодняшнего занятия 11А и списание
const today = new Date(); const from = new Date(today.getFullYear(), today.getMonth(), today.getDate());
const lessons = (await A('GET', `/lessons?group_id=${g11a.id}&from=${from.toISOString()}&to=${new Date(+from + 864e5).toISOString()}`)).body.data;
const lesson = lessons[0];
if (lesson && new Date(lesson.starts_at) < new Date(Date.now() + 30 * 60e3)) {
  const roster = (await A('GET', `/lessons/${lesson.id}/attendance`)).body.data.roster;
  const items = roster.filter(x => x.student_id).map(x => ({ student_id: x.student_id, status: x.student_id === sid ? 'present' : 'present' }));
  r = await A('PUT', `/lessons/${lesson.id}/attendance`, { items, close: true, topic: 'Тест' });
  check('закрытие занятия со списаниями', r.status === 200 && r.body.data.status === 'done', JSON.stringify(r.body));
  card = (await A('GET', '/students/' + sid)).body.data;
  const sub = card.subscriptions.find(s => s.lessons_total === 12 && s.status !== 'cancelled');
  check('занятие списано с абонемента (1 из 12)', sub?.lessons_used === 1, JSON.stringify(sub));
  r = await A('PUT', `/lessons/${lesson.id}/attendance`, { items: [{ student_id: sid, status: 'excused', reason: 'справка' }] });
  card = (await A('GET', '/students/' + sid)).body.data;
  const sub2 = card.subscriptions.find(s => s.id === sub.id);
  check('смена на «уважительная» → корректирующий возврат занятия', sub2.lessons_used === 0, JSON.stringify(sub2));
} else console.log('  (занятие 11А сегодня ещё не началось — проверка списаний пропущена)');

// преподаватель
console.log('Преподаватель');
const T = api((await login('erlan@bilimplus.kz')).token);
r = await T('GET', '/students');
check('преподаватель не видит список учеников (403)', r.status === 403);
r = await T('GET', '/payments');
check('преподаватель не видит финансы (403)', r.status === 403);
r = await T('GET', `/lessons?from=${from.toISOString()}&to=${new Date(+from + 864e5).toISOString()}`);
check('преподаватель видит только свои занятия', r.status === 200 && r.body.data.length > 0 && r.body.data.every(l => l.teacher_name === 'Ерлан Абенов'), r.body.data?.map(l => l.teacher_name).join());
const others = (await A('GET', `/lessons?from=${from.toISOString()}&to=${new Date(+from + 864e5).toISOString()}`)).body.data.filter(l => l.teacher_name !== 'Ерлан Абенов');
r = await T('GET', `/lessons/${others[0].id}/attendance`);
check('чужое занятие недоступно преподавателю (404)', r.status === 404);

// изоляция организаций
console.log('Изоляция организаций');
r = await fetch(B + '/auth/register', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ full_name: 'Другой Владелец', phone: '87019998877', email: `other${Date.now()}@test.kz`, password: 'Password1234', accept_terms: true, center_name: 'Другой центр', center_type: 'language', city: 'Астана' }) });
const reg = await r.json();
check('регистрация новой организации', r.status === 200 && reg.data.access_token);
const O = api(reg.data.access_token);
r = await O('GET', '/students/' + sid);
check('чужой ученик по ID недоступен (404)', r.status === 404);
r = await O('GET', '/students');
check('список учеников новой организации пуст', r.body.data.length === 0);
r = await O('POST', '/students', { full_name: 'Взлом', branch_id: branchId });
check('нельзя создать ученика в чужом филиале', r.status >= 400);
r = await O('GET', '/custom-fields?entity=student');
check('шаблон отрасли «Языковая школа» применён', r.body.data.some(f => f.key === 'level'));
r = await O('GET', '/platform/organizations');
check('владелец центра не имеет доступа к Super Admin (403)', r.status === 403);

console.log('Super Admin');
const SA = api((await login('admin@emp.kz')).token);
r = await SA('GET', '/platform/organizations');
check('список организаций', r.status === 200 && r.body.data.length >= 9);
r = await SA('GET', '/students');
check('Super Admin не видит учеников организаций (403)', r.status === 403);

// refresh-токен: ротация и повторное использование
console.log('Сессии');
const s1 = await login('aigerim@bilimplus.kz');
const c1 = s1.cookie.split(';')[0];
let rr = await fetch(B + '/auth/refresh', { method: 'POST', headers: { Cookie: c1 } });
check('refresh выдаёт новый токен', rr.status === 200);
rr = await fetch(B + '/auth/refresh', { method: 'POST', headers: { Cookie: c1 } });
check('повторное использование отозванного refresh → 401', rr.status === 401);

console.log(`\nИтого: ${ok} пройдено, ${fail} не пройдено`);
process.exit(fail ? 1 : 0);
