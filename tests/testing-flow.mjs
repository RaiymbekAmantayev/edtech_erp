// Сценарный тест «Тестирование и КТП». Запуск: node tests/testing-flow.mjs при запущенном API с демо-данными.
// Создаёт навыки, оценки, типы тестов, КТП и закрывает одно прошедшее занятие преподавателя (после — migrate -reset -seed).
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

console.log('Настройки тестирования (владелец)');
const skills = [];
for (const n of ['Grammar', 'Listening', 'Speaking']) skills.push((await A('POST', '/test-skills', { name: `${n} ${tag}` })).body.data.id);
check('навыки созданы', skills.every(Boolean));
check('пустое название навыка → 422', (await A('POST', '/test-skills', { name: '  ' })).status === 422);
const gA = (await A('POST', '/test-grades', { name: `Отлично ${tag}`, color: '#16a34a', min_percent: 85 })).body.data.id;
await A('POST', '/test-grades', { name: `Хорошо ${tag}`, min_percent: 60 });
check('порог больше 100% → 422', (await A('POST', '/test-grades', { name: 'X', min_percent: 120 })).status === 422);

// занятие преподавателя, которое надо отметить, и его курс
const cab = (await T('GET', '/teacher/cabinet')).body.data;
const lesson = cab.to_mark[0];
check('у преподавателя есть занятие для отметки', !!lesson);
const att0 = (await T('GET', `/lessons/${lesson.id}/attendance`)).body.data;
const groups = (await A('GET', '/groups')).body.data;
const courseId = groups.find((g) => g.id === att0.lesson.group_id)?.course_id;
const otherCourse = groups.find((g) => g.course_id && g.course_id !== courseId)?.course_id;

let r = await A('POST', '/test-types', { name: `Тест урока ${tag}`, scoring: 'score', lesson_mode: 'required', course_ids: [courseId], skills: skills.map((s) => ({ skill_id: s, max_score: 10 })) });
const required = r.body.data?.id;
r = await A('POST', '/test-types', { name: `Словарный ${tag}`, scoring: 'grade', lesson_mode: 'optional', course_ids: [], skills: [{ skill_id: skills[0], max_score: 1 }] });
const optional = r.body.data?.id;
r = await A('POST', '/test-types', { name: `Чужой курс ${tag}`, lesson_mode: 'required', course_ids: [otherCourse], skills: [{ skill_id: skills[1], max_score: 5 }] });
const foreign = r.body.data?.id;
r = await A('POST', '/test-types', { name: `Входной ${tag}`, lesson_mode: 'off', skills: [{ skill_id: skills[0], max_score: 5 }] });
const offType = r.body.data?.id;
check('типы тестов созданы', required && optional && foreign && offType);
check('неверный режим урока → 422', (await A('POST', '/test-types', { name: 'X', lesson_mode: 'always', skills: [{ skill_id: skills[0], max_score: 1 }] })).status === 422);
check('преподаватель не меняет настройки → 403', (await T('POST', '/test-skills', { name: 'hack' })).status === 403);
const tcfg = (await T('GET', '/tests/config')).body.data;
check('преподаватель видит только тесты на уроке', tcfg.types.some((t) => t.id === required) && !tcfg.types.some((t) => t.id === offType));

console.log('КТП (владелец)');
r = await A('POST', '/ktp/categories', { name: `КТП ${tag}`, course_ids: [courseId] });
const cat = r.body.data?.id;
check('категория КТП создана', r.status === 201);
r = await A('POST', `/ktp/categories/${cat}/topics`, { items: [
  { title: 'Present Simple', homework: 'Упр. 1–3, с. 14', hours: 1 },
  { title: 'Past Simple', homework: 'Упр. 4, с. 20' },
  { title: 'Future Simple' },
] });
check('темы добавлены списком', r.status === 201 && r.body.data.ids.length === 3);
const topics = r.body.data.ids;
check('тема без названия → 422', (await A('POST', `/ktp/categories/${cat}/topics`, { title: '' })).status === 422);
check('преподаватель не меняет КТП → 403', (await T('GET', '/ktp')).status === 403);

console.log('Отметка урока (преподаватель)');
const att = (await T('GET', `/lessons/${lesson.id}/attendance`)).body.data;
const ktpCat = att.ktp.categories.find((c) => c.id === cat);
check('на уроке — КТП курса с ДЗ', ktpCat && ktpCat.topics.length === 3 && ktpCat.topics[0].homework === 'Упр. 1–3, с. 14');
check('следующая тема по плану — первая', att.ktp.suggested_topic_id === topics[0]);
check('тесты урока: по курсу и для всех курсов', att.tests.some((t) => t.id === required && t.required) && att.tests.some((t) => t.id === optional && !t.required));
check('тест чужого курса и «не на уроке» не показываются', !att.tests.some((t) => t.id === foreign || t.id === offType));

const students = att.roster.filter((x) => x.student_id);
const items = att.roster.map((x, i) => ({ student_id: x.student_id ?? '', lead_id: x.lead_id ?? '', status: i === 0 && students.length > 1 ? 'absent' : x.is_trial ? 'trial' : 'present' }));
const present = students.filter((x) => items.find((i) => i.student_id === x.student_id).status !== 'absent');
const absent = students.find((x) => items.find((i) => i.student_id === x.student_id).status === 'absent');
const base = { items, topic: 'Present Simple', homework: 'Упр. 1–3, с. 14', ktp_topic_id: topics[0], close: true };

r = await T('PUT', `/lessons/${lesson.id}/attendance`, { ...base, tests: [] });
check('без обязательного теста занятие не закрывается → 422', r.status === 422 && r.body.error.code === 'LESSON_TESTS_REQUIRED', JSON.stringify(r.body));
r = await T('PUT', `/lessons/${lesson.id}/attendance`, { ...base, tests: present.map((x) => ({ student_id: x.student_id, test_type_id: required, scores: [{ skill_id: skills[0], score: 11 }] })) });
check('балл больше максимума → 422', r.status === 422);
r = await T('PUT', `/lessons/${lesson.id}/attendance`, { ...base, tests: [
  ...present.map((x, i) => ({ student_id: x.student_id, test_type_id: required, scores: skills.map((s) => ({ skill_id: s, score: i === 0 ? 9 : 7 })) })),
  { student_id: present[0].student_id, test_type_id: optional, scores: [{ skill_id: skills[0], grade_id: gA }] },
  ...(absent ? [{ student_id: absent.student_id, test_type_id: required, scores: [{ skill_id: skills[0], score: 5 }] }] : []),
] });
check('урок закрыт с темой, ДЗ и баллами', r.status === 200 && r.body.data.status === 'done', JSON.stringify(r.body));

const after = (await T('GET', `/lessons/${lesson.id}/attendance`)).body.data;
check('тема и ДЗ сохранены', after.lesson.topic === 'Present Simple' && after.lesson.homework === 'Упр. 1–3, с. 14' && after.lesson.ktp_topic_id === topics[0]);
check('баллы сохранены (отсутствующему не ставятся)', after.results.filter((e) => e.test_type_id === required).length === present.length && (!absent || !after.results.some((e) => e.student_id === absent.student_id)));
check('пройденная тема отмечена, следующая — вторая', after.ktp.categories.find((c) => c.id === cat).topics[0].done && after.ktp.suggested_topic_id === topics[1]);

const res = (await A('GET', '/tests?student_id=' + present[0].student_id)).body.data;
const lr = res.find((x) => x.lesson_id === lesson.id && x.test_type_id === required);
check('результат за урок в карточке: 27/30 = 90% → «Отлично»', lr && lr.total_score === 27 && lr.percent === 90 && lr.grade?.name.startsWith('Отлично'), JSON.stringify(lr));

// пересохранение заменяет, а не дублирует результаты
r = await T('PUT', `/lessons/${lesson.id}/attendance`, { ...base, close: false, tests: present.map((x) => ({ student_id: x.student_id, test_type_id: required, scores: skills.map((s) => ({ skill_id: s, score: 10 })) })) });
const again = (await T('GET', `/lessons/${lesson.id}/attendance`)).body.data;
check('повторное сохранение заменяет баллы без дублей', r.status === 200 && again.results.filter((e) => e.test_type_id === required).length === present.length
  && again.results.every((e) => e.test_type_id !== optional));

check('преподаватель не вносит тест вне урока → 403', (await T('POST', '/tests', { test_type_id: required, taken_on: today, items: [{ student_id: present[0].student_id, scores: [{ skill_id: skills[0], score: 1 }] }] })).status === 403);
r = await A('POST', '/tests', { test_type_id: offType, taken_on: today, items: [{ student_id: present[0].student_id, scores: [{ skill_id: skills[0], score: 4 }] }] });
check('админ вносит тест вне урока в карточке', r.status === 201);
check('нельзя менять способ оценки при наличии результатов → 409', (await A('PATCH', '/test-types/' + required, { scoring: 'grade' })).status === 409);
r = await A('PATCH', '/test-types/' + optional, { lesson_mode: 'off' });
check('выключение теста на уроке скрывает его у преподавателя', r.status === 200 && !(await T('GET', `/lessons/${lesson.id}/attendance`)).body.data.tests.some((t) => t.id === optional));

console.log(`\n${ok} ✔  ${fail} ✘`);
process.exit(fail ? 1 : 0);
