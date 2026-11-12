// Сценарный тест модуля «Тестирование». Запуск: node tests/testing-flow.mjs при запущенном API с демо-данными.
// Создаёт навыки, оценки, типы тестов и результаты в демо-центре Bilim Plus.
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
  let j = null; try { j = t ? JSON.parse(t) : null } catch { /* пусто */ }
  return { status: r.status, body: j };
};
const today = new Date().toISOString().slice(0, 10);
const tag = Date.now().toString(36).slice(-4);

const A = api(await login('aigerim@bilimplus.kz'));
const T = api(await login('erlan@bilimplus.kz'));

console.log('Настройки (владелец)');
const skills = []
for (const n of ['Grammar', 'Listening', 'Speaking']) skills.push((await A('POST', '/test-skills', { name: `${n} ${tag}` })).body.data.id);
check('навыки созданы', skills.every(Boolean));
let r = await A('POST', '/test-skills', { name: '  ' });
check('пустое название навыка → 422', r.status === 422);
const gA = (await A('POST', '/test-grades', { name: `Отлично ${tag}`, color: '#16a34a', min_percent: 85 })).body.data.id;
const gB = (await A('POST', '/test-grades', { name: `Хорошо ${tag}`, min_percent: 60 })).body.data.id;
r = await A('POST', '/test-grades', { name: 'X', min_percent: 120 });
check('порог больше 100% → 422', r.status === 422);

r = await A('POST', '/test-types', { name: `Промежуточный ${tag}`, scoring: 'score', show_in_miniapp: true, skills: skills.map((s) => ({ skill_id: s, max_score: 10 })) });
const visible = r.body.data.id;
r = await A('POST', '/test-types', { name: `Входной ${tag}`, scoring: 'grade', show_in_miniapp: false, skills: [{ skill_id: skills[0], max_score: 1 }] });
const hidden = r.body.data.id;
check('типы тестов созданы', visible && hidden);
r = await A('POST', '/test-types', { name: 'Без навыков', skills: [] });
check('тип без навыков → 422', r.status === 422);

let cfg = (await A('GET', '/tests/config')).body.data;
check('админ видит оба типа', cfg.types.some((t) => t.id === visible) && cfg.types.some((t) => t.id === hidden));
check('навыки типа с max_score', cfg.types.find((t) => t.id === visible).skills.length === 3);

console.log('Преподаватель');
const T401 = await T('POST', '/test-skills', { name: 'hack' });
check('преподаватель не меняет настройки → 403', T401.status === 403);
cfg = (await T('GET', '/tests/config')).body.data;
check('преподаватель видит только типы для Mini App', cfg.types.some((t) => t.id === visible) && !cfg.types.some((t) => t.id === hidden));
const tg = (await T('GET', '/tests/groups')).body.data;
check('у преподавателя есть свои группы', tg.length > 0 && tg.every((g) => g.teacher === 'Ерлан Абенов'), JSON.stringify(tg));
const group = tg.find((g) => g.students > 0);
const roster = (await T('GET', '/tests/roster?group_id=' + group.id)).body.data;
check('состав группы', roster.students.length > 0);
const st = roster.students[0];
r = await T('POST', '/tests', { test_type_id: hidden, group_id: group.id, taken_on: today, items: [{ student_id: st.id, scores: [{ skill_id: skills[0], grade_id: gA }] }] });
check('скрытый тип недоступен преподавателю → 403', r.status === 403, JSON.stringify(r.body));
r = await T('POST', '/tests', { test_type_id: visible, group_id: group.id, taken_on: today, items: [{ student_id: st.id, scores: [{ skill_id: skills[0], score: 11 }] }] });
check('балл больше максимума → 422', r.status === 422);
r = await T('POST', '/tests', { test_type_id: visible, group_id: group.id, taken_on: today, items: roster.students.slice(0, 2).map((s, i) => ({
  student_id: s.id, scores: skills.map((sk) => ({ skill_id: sk, score: i === 0 ? 9 : 7 })), comment: i === 0 ? 'Молодец' : '' })) });
check('результаты группы сохранены', r.status === 201 && r.body.data.ids.length === Math.min(2, roster.students.length), JSON.stringify(r.body));
const createdIds = r.body.data.ids;

const others = (await A('GET', '/groups')).body.data.filter((g) => g.teacher_id && !tg.some((x) => x.id === g.id));
if (others.length) {
  r = await T('GET', '/tests/roster?group_id=' + others[0].id);
  check('чужая группа недоступна → 403', r.status === 403);
}

const mine = (await T('GET', '/tests?group_id=' + group.id)).body.data;
const first = mine.find((x) => x.id === createdIds[0]);
check('итог 27/30 = 90% → авто-оценка «Отлично»', first && first.total_score === 27 && first.max_score === 30 && first.percent === 90 && first.grade?.name.startsWith('Отлично'), JSON.stringify(first));
const second = mine.find((x) => x.id === createdIds[1]);
if (second) check('21/30 = 70% → «Хорошо»', second.percent === 70 && second.grade?.name.startsWith('Хорошо'), JSON.stringify(second));

console.log('Карточка ученика (владелец)');
const byStudent = (await A('GET', '/tests?student_id=' + st.id)).body.data;
check('результат виден в карточке', byStudent.some((x) => x.id === createdIds[0] && x.scores.length === 3 && x.comment === 'Молодец'));
r = await A('POST', '/tests', { test_type_id: hidden, taken_on: today, items: [{ student_id: st.id, scores: [{ skill_id: skills[0], grade_id: gB }], grade_id: gB }] });
check('админ вносит тест по оценкам без группы', r.status === 201, JSON.stringify(r.body));
r = await A('PATCH', '/test-types/' + hidden, { scoring: 'score' });
check('нельзя менять способ оценки при наличии результатов → 409', r.status === 409);
r = await A('PATCH', '/test-types/' + hidden, { show_in_miniapp: true });
cfg = (await T('GET', '/tests/config')).body.data;
check('включение в Mini App открывает тип преподавателю', r.status === 200 && cfg.types.some((t) => t.id === hidden));
await A('PATCH', '/test-types/' + hidden, { archived: true });
cfg = (await T('GET', '/tests/config')).body.data;
check('архивный тип скрыт', !cfg.types.some((t) => t.id === hidden));

r = await T('DELETE', '/tests/' + createdIds[0]);
check('преподаватель удаляет свой результат', r.status === 204);

console.log(`\n${ok} ✔  ${fail} ✘`);
process.exit(fail ? 1 : 0);
