// Пакет seed — демо-данные для локального запуска (повторяют макеты интерфейса).
package seed

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"time"

	"emp/internal/modules/attendance"
	"emp/internal/modules/finance"
	"emp/internal/modules/organization"
	"emp/internal/modules/schedule"
	"emp/internal/platform/auth"
	"emp/internal/platform/database"

	"github.com/jackc/pgx/v5"
)

const DemoPassword = "Demo12345!"

var rnd = rand.New(rand.NewSource(42))

func Run(ctx context.Context, db *database.DB) error {
	var n int
	db.Sys.QueryRow(ctx, `SELECT count(*) FROM tenants`).Scan(&n)
	if n > 0 {
		slog.Info("seed skipped: database already has organizations")
		return nil
	}
	hash, err := auth.HashPassword(DemoPassword)
	if err != nil {
		return err
	}
	if _, err := db.Sys.Exec(ctx, `INSERT INTO users (email, full_name, password_hash, is_platform_admin) VALUES ('admin@emp.kz', 'Super Admin', $1, true)`, hash); err != nil {
		return err
	}
	if err := seedMain(ctx, db, hash); err != nil {
		return fmt.Errorf("main tenant: %w", err)
	}
	for _, o := range otherOrgs {
		if err := seedOrg(ctx, db, hash, o); err != nil {
			return fmt.Errorf("org %s: %w", o.name, err)
		}
	}
	slog.Info("seed complete", "owner", "aigerim@bilimplus.kz", "teacher", "erlan@bilimplus.kz", "super_admin", "admin@emp.kz", "password", DemoPassword)
	return nil
}

func createTenant(ctx context.Context, db *database.DB, name, ctype, city, plan, status string, created time.Time, trialEnds, paidUntil *time.Time) (string, error) {
	var id string
	settings, _ := json.Marshal(organization.DefaultSettings("medium"))
	err := db.Sys.QueryRow(ctx, `INSERT INTO tenants (name, center_type, city, plan_code, status, trial_ends_at, paid_until, created_at, settings, bin)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`, name, ctype, city, plan, status, trialEnds, paidUntil, created, settings,
		fmt.Sprintf("%012d", 190540000000+rnd.Int63n(99999))).Scan(&id)
	return id, err
}

func ptrTime(t time.Time) *time.Time { return &t }

// ---------------------------------------------------------------- основной центр

type ids map[string]string

func seedMain(ctx context.Context, db *database.DB, hash string) error {
	loc, _ := time.LoadLocation("Asia/Almaty")
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	tenantID, err := createTenant(ctx, db, "Bilim Plus", "ent", "Алматы", "business", "active", time.Date(2026, 3, 3, 10, 0, 0, 0, loc), nil, ptrTime(today.AddDate(0, 0, 28)))
	if err != nil {
		return err
	}
	return db.InTenant(ctx, tenantID, func(tx pgx.Tx) error {
		ownerRole, err := organization.Bootstrap(ctx, tx, tenantID, "ent")
		if err != nil {
			return err
		}
		roles := ids{}
		rows, _ := tx.Query(ctx, `SELECT code, id::text FROM roles`)
		for rows.Next() {
			var c, i string
			rows.Scan(&c, &i)
			roles[c] = i
		}
		rows.Close()
		_ = ownerRole

		// филиалы и кабинеты
		var b1, b2 string
		tx.QueryRow(ctx, `INSERT INTO branches (tenant_id, name, address, phone) VALUES ($1,'Алматы, Абая 52','пр. Абая, 52','+77273001122') RETURNING id`, tenantID).Scan(&b1)
		tx.QueryRow(ctx, `INSERT INTO branches (tenant_id, name, address, phone) VALUES ($1,'Алматы, Самал-2','мкр. Самал-2, 58','+77273001133') RETURNING id`, tenantID).Scan(&b2)
		rooms := ids{}
		for i, r := range []struct {
			name   string
			cap    *int
			online bool
		}{{"Каб. 101", ip(14), false}, {"Каб. 204", ip(10), false}, {"Каб. 305", ip(8), false}, {"IT-класс", ip(12), false}, {"Онлайн", nil, true}} {
			var id string
			tx.QueryRow(ctx, `INSERT INTO classrooms (tenant_id, branch_id, name, capacity, is_online, sort_order, equipment) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
				tenantID, b1, r.name, r.cap, r.online, i, map[bool]string{true: "Zoom", false: "Проектор, доска"}[r.online]).Scan(&id)
			rooms[r.name] = id
		}
		var roomSamal string
		tx.QueryRow(ctx, `INSERT INTO classrooms (tenant_id, branch_id, name, capacity) VALUES ($1,$2,'Каб. 1',10) RETURNING id`, tenantID, b2).Scan(&roomSamal)

		// пользователи
		mkUser := func(email, name, phone string, roleCodes ...string) string {
			var id string
			tx.QueryRow(ctx, `INSERT INTO users (tenant_id, email, phone, full_name, password_hash, last_login_at) VALUES ($1,$2,$3,$4,$5,NOW()) RETURNING id`,
				tenantID, email, phone, name, hash).Scan(&id)
			for _, rc := range roleCodes {
				tx.Exec(ctx, `INSERT INTO user_roles (tenant_id, user_id, role_id) VALUES ($1,$2,$3)`, tenantID, id, roles[rc])
				if rc == "teacher" || rc == "sales" || rc == "branch_manager" {
					tx.Exec(ctx, `INSERT INTO user_branch_access (tenant_id, user_id, branch_id) VALUES ($1,$2,$3)`, tenantID, id, b1)
				}
			}
			return id
		}
		owner := mkUser("aigerim@bilimplus.kz", "Айгерим Касымова", "+77017001010", "owner")
		erlanUser := mkUser("erlan@bilimplus.kz", "Ерлан Абенов", "+77017002020", "teacher")
		salesUser := mkUser("madi@bilimplus.kz", "Мади Серикбаев", "+77017003030", "sales")
		mkUser("buh@bilimplus.kz", "Гульмира Ахметова", "+77017004040", "accountant")

		// преподаватели
		teachers := ids{}
		for _, t := range []struct {
			name, phone string
			user        *string
			subjects    []string
			rate        int64
		}{
			{"Ерлан Абенов", "+77017002020", &erlanUser, []string{"Математика", "Физика", "История"}, 600000},
			{"Динара Сейткали", "+77051112233", nil, []string{"Английский язык"}, 550000},
			{"Анна Ким", "+77012223344", nil, []string{"IELTS", "Английский язык"}, 700000},
			{"Тимур Жумабаев", "+77473334455", nil, []string{"Python", "Web"}, 600000},
			{"Асель Нуркенова", "+77084445566", nil, []string{"Английский для детей"}, 450000},
		} {
			var id string
			tx.QueryRow(ctx, `INSERT INTO employees (tenant_id, user_id, full_name, phone, position, subjects, branch_ids, hired_at, pay_type, pay_rate_tiyn)
				VALUES ($1,$2,$3,$4,'Преподаватель',$5,ARRAY[$6]::uuid[],'2025-08-20','per_lesson',$7) RETURNING id`,
				tenantID, t.user, t.name, t.phone, t.subjects, b1, t.rate).Scan(&id)
			teachers[t.name] = id
		}

		// курсы
		courses := ids{}
		for _, c := range []struct {
			name, cat string
			dur, cnt  int
			price     int64
		}{
			{"ЕНТ Математика", "ЕНТ", 90, 12, 7200000}, {"ЕНТ Физика", "ЕНТ", 90, 12, 7200000}, {"ЕНТ История", "ЕНТ", 60, 8, 4800000},
			{"English A2", "Английский", 90, 12, 4800000}, {"English B1", "Английский", 90, 8, 4400000}, {"IELTS Intensive", "Английский", 90, 8, 6400000},
			{"English Kids", "Английский", 90, 8, 3600000}, {"Python Junior", "IT", 60, 8, 4000000}, {"Web Start", "IT", 90, 8, 4800000},
			{"Индивидуальные занятия", "Индивидуально", 60, 8, 8000000},
		} {
			var id string
			tx.QueryRow(ctx, `INSERT INTO courses (tenant_id, name, category, duration_minutes, lessons_count, price_tiyn) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
				tenantID, c.name, c.cat, c.dur, c.cnt, c.price).Scan(&id)
			courses[c.name] = id
		}
		// «Целевой балл ЕНТ» и «Класс» — только для курсов ЕНТ
		entCourses := []string{courses["ЕНТ Математика"], courses["ЕНТ Физика"], courses["ЕНТ История"]}
		scope, _ := json.Marshal(entCourses)
		tx.Exec(ctx, `UPDATE custom_field_definitions SET settings = settings || jsonb_build_object('course_ids', $1::jsonb) WHERE key IN ('grade','target_score','trial_score','subjects')`, scope)
		tx.Exec(ctx, `UPDATE custom_field_definitions SET settings = settings || '{"required":"on_status","required_status":"active"}' WHERE key='grade'`)

		cfOpt := func(field, label string) string {
			var id string
			tx.QueryRow(ctx, `SELECT o.id FROM custom_field_options o JOIN custom_field_definitions d ON d.id=o.field_id WHERE d.key=$1 AND d.entity_type='student' AND o.label->>'ru'=$2`, field, label).Scan(&id)
			return id
		}

		// группы и расписание (понедельник 02.11 — как на макете)
		type grp struct {
			name, course, teacher, room string
			weekdays                    []int
			start, end                  string
			capacity                    int
			size                        int
			branch                      string
		}
		groupsDef := []grp{
			{"English A2 · утро", "English A2", "Динара Сейткали", "Каб. 204", []int{1, 3, 5}, "09:00", "10:30", 10, 8, b1},
			{"ЕНТ Математика 11А", "ЕНТ Математика", "Ерлан Абенов", "Каб. 101", []int{1, 3, 5}, "11:00", "12:30", 14, 12, b1},
			{"ЕНТ Математика 11Б", "ЕНТ Математика", "Ерлан Абенов", "Каб. 101", []int{2, 4}, "11:00", "12:30", 14, 10, b1},
			{"IELTS Intensive", "IELTS Intensive", "Анна Ким", "Каб. 305", []int{1, 3}, "14:00", "15:30", 8, 6, b1},
			{"Python Junior", "Python Junior", "Тимур Жумабаев", "IT-класс", []int{1, 4}, "15:00", "16:00", 12, 9, b1},
			{"ЕНТ Физика 10Б", "ЕНТ Физика", "Ерлан Абенов", "Каб. 101", []int{1, 3}, "16:30", "18:00", 14, 11, b1},
			{"Web Start", "Web Start", "Тимур Жумабаев", "IT-класс", []int{1, 3}, "16:30", "18:00", 12, 10, b1},
			{"English Kids 3", "English Kids", "Асель Нуркенова", "Каб. 204", []int{1, 3}, "17:00", "18:30", 10, 9, b1},
			{"English B1 · вечер", "English B1", "Динара Сейткали", "Онлайн", []int{1, 3}, "18:30", "20:00", 12, 10, b1},
			{"IELTS Evening", "IELTS Intensive", "Анна Ким", "Каб. 305", []int{1, 3}, "18:30", "20:00", 8, 7, b1},
			{"English Kids 1", "English Kids", "Асель Нуркенова", "", []int{2, 4}, "16:00", "17:30", 10, 8, b2},
		}
		groups := ids{}
		groupCourse := ids{}
		for _, g := range groupsDef {
			var id string
			room := rooms[g.room]
			if g.branch == b2 {
				room = roomSamal
			}
			if err := tx.QueryRow(ctx, `INSERT INTO groups (tenant_id, branch_id, course_id, name, teacher_id, classroom_id, start_date, capacity) VALUES ($1,$2,$3,$4,$5,$6,'2026-09-01',$7) RETURNING id`,
				tenantID, g.branch, courses[g.course], g.name, teachers[g.teacher], room, g.capacity).Scan(&id); err != nil {
				return err
			}
			groups[g.name] = id
			groupCourse[id] = courses[g.course]
			var sid string
			if err := tx.QueryRow(ctx, `INSERT INTO schedules (tenant_id, group_id, weekdays, start_time, end_time, valid_from) VALUES ($1,$2,$3,$4::time,$5::time,'2026-10-05') RETURNING id`,
				tenantID, id, g.weekdays, g.start, g.end).Scan(&sid); err != nil {
				return err
			}
		}
		// ЕНТ История онлайн: Ерлан одновременно в каб. 101 и онлайн — конфликт, как на макете
		var histID string
		tx.QueryRow(ctx, `INSERT INTO groups (tenant_id, branch_id, course_id, name, teacher_id, classroom_id, start_date, capacity) VALUES ($1,$2,$3,'ЕНТ История онлайн',$4,$5,'2026-10-26',30) RETURNING id`,
			tenantID, b1, courses["ЕНТ История"], teachers["Ерлан Абенов"], rooms["Онлайн"]).Scan(&histID)
		groups["ЕНТ История онлайн"] = histID
		groupCourse[histID] = courses["ЕНТ История"]
		tx.Exec(ctx, `INSERT INTO schedules (tenant_id, group_id, weekdays, start_time, end_time, valid_from) VALUES ($1,$2,'{1}','16:30','17:30',$3)`, tenantID, histID, today.Format("2006-01-02"))

		// организация: 17 декабря — корпоративный выходной (вместе с 16.12 — «нерабочие дни» на макете)
		tx.Exec(ctx, `INSERT INTO calendar_holidays (tenant_id, day, name) VALUES ($1,'2026-12-17','Корпоративный выходной')`, tenantID)

		// ученики
		type st struct {
			name, phone, school string
			grade               string
			target              int
			status              string
			subjects            []string
		}
		ent11A := []st{
			{"Алимжан Нурлан", "+77012451890", "НИШ ФМН", "11", 128, "active", []string{"Математика", "Физика"}},
			{"Бекова Аружан", "+77073310244", "Школа-лицей №165", "11", 120, "active", []string{"Математика", "Информатика"}},
			{"Габдуллин Санжар", "+77751187703", "Гимназия №56", "11", 115, "frozen", []string{"Математика", "Физика"}},
			{"Жакупова Томирис", "+77056719028", "НИШ ФМН", "11", 135, "active", []string{"Математика", "Физика"}},
			{"Сапарова Дана", "+77772135641", "КТЛ", "11", 130, "active", []string{"Математика", "Информатика"}},
			{"Нуртаев Алихан", "+77012001122", "Школа №12", "11", 112, "active", []string{"Математика", "География"}},
			{"Омарова Айым", "+77013002233", "Гимназия №159", "11", 118, "active", []string{"Математика", "Физика"}},
			{"Рахимов Темирлан", "+77014003344", "НИШ ФМН", "11", 126, "active", []string{"Математика", "Физика"}},
			{"Сейтжан Арсен", "+77015004455", "РФМШ", "11", 132, "active", []string{"Математика", "Физика"}},
			{"Токтарова Жания", "+77016005566", "Школа №134", "11", 109, "active", []string{"Математика", "Информатика"}},
			{"Утепов Ерасыл", "+77017006677", "КТЛ", "11", 121, "active", []string{"Математика", "Физика"}},
			{"Хамитова Камила", "+77018007788", "Школа-лицей №165", "11", 117, "active", []string{"Математика", "География"}},
		}
		ent11B := []st{
			{"Досжанова Мадина", "+77029081156", "РФМШ", "11", 125, "active", []string{"Математика", "Физика"}},
			{"Ермеков Даурен", "+77475206412", "Школа №12", "11", 110, "active", []string{"Математика", "Информатика"}},
			{"Мухамеджанов Арман", "+77084551290", "Школа №134", "11", 112, "active", []string{"Математика", "География"}},
			{"Тулегенов Ильяс", "+77003892516", "Школа-лицей №165", "11", 122, "active", []string{"Математика", "Физика"}},
		}
		var allStudents []string
		studentGroup := map[string]string{}
		parentsCount := 0
		insertStudent := func(s st, groupID, branch string, joined time.Time) string {
			cf := map[string]any{}
			if s.grade != "" {
				cf["grade"] = cfOpt("grade", s.grade)
				cf["school"] = s.school
				cf["target_score"] = s.target
				cf["trial_score"] = s.target - 15 - rnd.Intn(15)
				subj := []string{}
				for _, x := range s.subjects {
					subj = append(subj, cfOpt("subjects", x))
				}
				cf["subjects"] = subj
				cf["university"] = []string{"КБТУ, Назарбаев Университет", "КазНУ", "SDU", "Satbayev University", "AITU"}[rnd.Intn(5)]
				cf["photo_consent"] = rnd.Intn(4) > 0
				cf["contract_number"] = fmt.Sprintf("Д-2026-%04d", 400+len(allStudents))
			}
			cfb, _ := json.Marshal(cf)
			var id string
			birth := time.Date(2009-rnd.Intn(2), time.Month(1+rnd.Intn(12)), 1+rnd.Intn(27), 0, 0, 0, 0, time.UTC)
			if s.grade == "" {
				birth = time.Date(2008-rnd.Intn(14), time.Month(1+rnd.Intn(12)), 1+rnd.Intn(27), 0, 0, 0, 0, time.UTC)
			}
			email := translitEmail(s.name)
			var iin *string
			if rnd.Intn(3) == 0 {
				v := makeIIN(birth)
				iin = &v
			}
			if err := tx.QueryRow(ctx, `INSERT INTO students (tenant_id, branch_id, full_name, birth_date, phone, email, iin, source, responsible_id, status, custom_fields, started_at, created_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`,
				tenantID, branch, s.name, birth, s.phone, email, iin, []string{"Instagram", "2GIS", "Рекомендация", "Сайт"}[rnd.Intn(4)], owner, s.status, cfb, joined.Format("2006-01-02"), joined).Scan(&id); err != nil {
				slog.Error("student", "err", err)
				return ""
			}
			tx.Exec(ctx, `INSERT INTO student_balances (tenant_id, student_id) VALUES ($1,$2)`, tenantID, id)
			if groupID != "" {
				tx.Exec(ctx, `INSERT INTO group_students (tenant_id, group_id, student_id, joined_at) VALUES ($1,$2,$3,$4)`, tenantID, groupID, id, joined)
				studentGroup[id] = groupID
			}
			// родители
			surname := strings.Fields(s.name)[0]
			mother := femaleSurname(surname) + " " + femaleNames[rnd.Intn(len(femaleNames))]
			var tg *int64
			if rnd.Intn(10) < 4 {
				v := int64(900000000 + parentsCount)
				tg = &v
			}
			var pid string
			tx.QueryRow(ctx, `INSERT INTO parents (tenant_id, full_name, phone, whatsapp, telegram_chat_id, telegram_link_code, consent_at, consent_method, consent_version)
				VALUES ($1,$2,$3,$3,$4,$5,$6,'form','v1') RETURNING id`, tenantID, mother, randPhone(), tg, auth.RandomCode(16), joined).Scan(&pid)
			tx.Exec(ctx, `INSERT INTO student_parents (tenant_id, student_id, parent_id, relation, is_payer, is_legal_rep) VALUES ($1,$2,$3,'mother',true,true)`, tenantID, id, pid)
			parentsCount++
			if rnd.Intn(2) == 0 {
				father := maleSurname(surname) + " " + maleNames[rnd.Intn(len(maleNames))]
				tx.QueryRow(ctx, `INSERT INTO parents (tenant_id, full_name, phone, whatsapp, telegram_link_code) VALUES ($1,$2,$3,$3,$4) RETURNING id`, tenantID, father, randPhone(), auth.RandomCode(16)).Scan(&pid)
				tx.Exec(ctx, `INSERT INTO student_parents (tenant_id, student_id, parent_id, relation) VALUES ($1,$2,$3,'father')`, tenantID, id, pid)
			}
			database.Audit(ctx, tx, "student", id, "create", nil, map[string]any{"full_name": s.name, "status": "registration"})
			allStudents = append(allStudents, id)
			return id
		}
		joinDate := func() time.Time { return time.Date(2026, 10, 5+rnd.Intn(15), 9, 0, 0, 0, loc) }
		for i, s := range ent11A {
			j := joinDate()
			if i == 0 {
				j = time.Date(2026, 10, 5, 9, 0, 0, 0, loc)
			}
			insertStudent(s, groups["ЕНТ Математика 11А"], b1, j)
		}
		for _, s := range ent11B {
			insertStudent(s, groups["ЕНТ Математика 11Б"], b1, joinDate())
		}
		for i := len(ent11B); i < 10; i++ {
			insertStudent(st{randomName(), randPhone(), schools[rnd.Intn(len(schools))], "11", 105 + rnd.Intn(30), "active", []string{"Математика", "Физика"}}, groups["ЕНТ Математика 11Б"], b1, joinDate())
		}
		for _, g := range groupsDef {
			if strings.HasPrefix(g.name, "ЕНТ Математика") {
				continue
			}
			for i := 0; i < g.size; i++ {
				s := st{name: randomName(), phone: randPhone(), status: "active"}
				if strings.HasPrefix(g.course, "ЕНТ") {
					s.grade, s.school, s.target, s.subjects = "10", schools[rnd.Intn(len(schools))], 100+rnd.Intn(35), []string{"Физика", "Математика"}
				}
				insertStudent(s, groups[g.name], g.branch, joinDate())
			}
		}
		for i := 0; i < 15; i++ {
			insertStudent(st{randomName(), randPhone(), schools[rnd.Intn(len(schools))], "11", 100 + rnd.Intn(35), "active", []string{"Всемирная история", "География"}}, histID, b1, today)
		}
		// регистрация без группы (Ким Виктория) и индивидуальные занятия
		kimID := insertStudent(st{"Ким Виктория", "+77010043877", "Гимназия №159", "11", 118, "registration", []string{"Математика", "Английский язык"}}, "", b1, today.AddDate(0, 0, -3))
		indivCourse := courses["Индивидуальные занятия"]
		mkIndiv := func(studentID, teacher, room string, weekday int, start, end string) {
			var gid, name string
			tx.QueryRow(ctx, `SELECT 'Индив. · ' || split_part(full_name,' ',1) || ' ' || left(split_part(full_name,' ',2),1) || '.' FROM students WHERE id=$1`, studentID).Scan(&name)
			tx.QueryRow(ctx, `INSERT INTO groups (tenant_id, branch_id, course_id, name, type, teacher_id, classroom_id, capacity) VALUES ($1,$2,$3,$4,'individual',$5,$6,1) RETURNING id`,
				tenantID, b1, indivCourse, name, teachers[teacher], rooms[room]).Scan(&gid)
			tx.Exec(ctx, `INSERT INTO group_students (tenant_id, group_id, student_id, joined_at) VALUES ($1,$2,$3,$4)`, tenantID, gid, studentID, today.AddDate(0, 0, -14))
			tx.Exec(ctx, `INSERT INTO schedules (tenant_id, group_id, weekdays, start_time, end_time, valid_from) VALUES ($1,$2,$3,$4::time,$5::time,$6)`,
				tenantID, gid, []int{weekday}, start, end, today.AddDate(0, 0, -14).Format("2006-01-02"))
			groupCourse[gid] = indivCourse
		}
		mkIndiv(kimID, "Динара Сейткали", "Каб. 204", 1, "13:00", "14:00")
		var saparova string
		tx.QueryRow(ctx, `SELECT id FROM students WHERE full_name='Сапарова Дана'`).Scan(&saparova)
		mkIndiv(saparova, "Ерлан Абенов", "Онлайн", 1, "10:00", "11:00")

		// генерация занятий на горизонт
		if _, err := schedule.ExtendAll(ctx, tx); err != nil {
			return err
		}

		// абонементы и первые оплаты: на дату вступления в группу
		studentCourse := func(sid string) (string, int64, int) {
			var cid string
			var price int64
			var cnt int
			tx.QueryRow(ctx, `SELECT c.id, c.price_tiyn, c.lessons_count FROM group_students gs JOIN groups g ON g.id=gs.group_id JOIN courses c ON c.id=g.course_id
				WHERE gs.student_id=$1 AND gs.left_at IS NULL ORDER BY g.type LIMIT 1`, sid).Scan(&cid, &price, &cnt)
			return cid, price, cnt
		}
		sell := func(sid string, start time.Time, payShare int) {
			cid, price, cnt := studentCourse(sid)
			if cid == "" {
				return
			}
			var cname, subID string
			tx.QueryRow(ctx, `SELECT name FROM courses WHERE id=$1`, cid).Scan(&cname)
			name := fmt.Sprintf("%d занятий · %s", cnt, cname)
			tx.QueryRow(ctx, `INSERT INTO subscriptions (tenant_id, student_id, course_id, type, name, price_tiyn, lessons_total, starts_on, ends_on, created_by, created_at)
				VALUES ($1,$2,$3,'lessons',$4,$5,$6,$7,$8,$9,$10) RETURNING id`, tenantID, sid, cid, name, price, cnt, start, start.AddDate(0, 0, 45), owner, start).Scan(&subID)
			finance.Post(ctx, tx, sid, "charge", -price, "subscription", &subID, "Абонемент: "+name, owner)
			if payShare > 0 {
				amount := price * int64(payShare) / 100
				var payID string
				method := []string{"kaspi", "kaspi", "kaspi", "cash", "card", "transfer"}[rnd.Intn(6)]
				paidAt := start.Add(time.Duration(rnd.Intn(8)) * time.Hour)
				tx.QueryRow(ctx, `INSERT INTO payments (tenant_id, student_id, amount_tiyn, paid_at, method, received_by, receipt_number, payer_parent_id)
					VALUES ($1,$2,$3,$4,$5,$6,$7,(SELECT parent_id FROM student_parents WHERE student_id=$2 AND is_payer LIMIT 1)) RETURNING id`,
					tenantID, sid, amount, paidAt, method, owner, fmt.Sprintf("%06d", rnd.Intn(999999))).Scan(&payID)
				finance.Post(ctx, tx, sid, "payment", amount, "payment", &payID, map[string]string{"kaspi": "Kaspi перевод", "cash": "Наличные", "card": "Карта", "transfer": "Банковский перевод"}[method], owner)
			}
		}
		for _, sid := range allStudents {
			var joined time.Time
			var status string
			tx.QueryRow(ctx, `SELECT started_at, status FROM students WHERE id=$1`, sid).Scan(&joined, &status)
			if status == "registration" {
				continue
			}
			share := 100
			switch r := rnd.Intn(10); {
			case r == 0:
				share = 50
			case r == 1:
				share = 0
			case r == 2:
				share = 125
			}
			sell(sid, joined, share)
		}

		// заморозка: Габдуллин Санжар до 15.11
		var sanzhar, sanzharSub string
		tx.QueryRow(ctx, `SELECT s.id, sb.id FROM students s JOIN subscriptions sb ON sb.student_id=s.id WHERE s.full_name='Габдуллин Санжар'`).Scan(&sanzhar, &sanzharSub)
		if sanzharSub != "" {
			tx.Exec(ctx, `INSERT INTO subscription_freezes (tenant_id, subscription_id, starts_on, ends_on, reason, created_by) VALUES ($1,$2,'2026-10-28','2026-11-15','Болезнь, справка',$3)`, tenantID, sanzharSub, owner)
			tx.Exec(ctx, `UPDATE subscriptions SET status='frozen', ends_on=ends_on+19 WHERE id=$1`, sanzharSub)
		}

		// проведение прошлых занятий: отметки + списания по правилам (как в API), продления абонементов
		lrows, err := tx.Query(ctx, `SELECT l.id, l.ends_at, g.name FROM lessons l JOIN groups g ON g.id=l.group_id WHERE l.ends_at < $1 AND l.status='planned' ORDER BY l.starts_at`, now)
		if err != nil {
			return err
		}
		type pl struct {
			id    string
			ends  time.Time
			group string
		}
		var past []pl
		for lrows.Next() {
			var x pl
			lrows.Scan(&x.id, &x.ends, &x.group)
			past = append(past, x)
		}
		lrows.Close()
		skipped := 0
		for _, l := range past {
			isToday := !l.ends.Before(today)
			// оставить неотмеченными: сегодняшние (кроме утренней English A2) и несколько прошлых
			if (isToday && l.group != "English A2 · утро") || (!isToday && skipped < 4 && rnd.Intn(40) == 0) {
				if !isToday {
					skipped++
				}
				continue
			}
			roster, err := attendance.Roster(ctx, tx, l.id)
			if err != nil {
				return err
			}
			for _, it := range roster {
				if it.StudentID == nil {
					continue
				}
				status := "present"
				switch r := rnd.Intn(100); {
				case r < 6:
					status = "absent"
				case r < 11:
					status = "late"
				case r < 14:
					status = "excused"
				}
				var frozen bool
				tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM subscription_freezes f JOIN subscriptions s ON s.id=f.subscription_id WHERE s.student_id=$1 AND $2::date BETWEEN f.starts_on AND f.ends_on)`,
					*it.StudentID, l.ends).Scan(&frozen)
				if frozen {
					status = "excused"
				}
				tx.Exec(ctx, `INSERT INTO attendance (tenant_id, lesson_id, student_id, status, reason, marked_by, marked_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
					tenantID, l.id, *it.StudentID, status, map[bool]*string{true: sp("справка"), false: nil}[status == "excused"], owner, l.ends.Add(5*time.Minute))
			}
			tx.Exec(ctx, `UPDATE lessons SET status='done', closed_at=$2, closed_by=$3, topic=$4 WHERE id=$1`, l.id, l.ends.Add(5*time.Minute), owner, topics[rnd.Intn(len(topics))])
			if _, err := attendance.ApplyCharges(ctx, tx, l.id, owner); err != nil {
				return err
			}
			// продление: когда абонемент израсходован, большинство родителей покупают новый
			urows, _ := tx.Query(ctx, `SELECT s.student_id FROM subscriptions s WHERE s.status='used'
				AND NOT EXISTS (SELECT 1 FROM subscriptions x WHERE x.student_id=s.student_id AND x.status='active')`)
			used, _ := pgx.CollectRows(urows, pgx.RowTo[string])
			for _, sid := range used {
				if rnd.Intn(10) < 8 {
					sell(sid, l.ends.Add(20*time.Hour), []int{100, 100, 100, 50}[rnd.Intn(4)])
				}
			}
		}

		// одна правка посещаемости «задним числом» — для истории в карточке
		var alim, lastAtt string
		tx.QueryRow(ctx, `SELECT id FROM students WHERE full_name='Алимжан Нурлан'`).Scan(&alim)
		tx.QueryRow(ctx, `SELECT a.id FROM attendance a JOIN lessons l ON l.id=a.lesson_id WHERE a.student_id=$1 ORDER BY l.starts_at DESC LIMIT 1`, alim).Scan(&lastAtt)
		if lastAtt != "" {
			tx.Exec(ctx, `UPDATE attendance SET status='excused', reason='справка' WHERE id=$1`, lastAtt)
			tx.Exec(ctx, `INSERT INTO attendance_history (tenant_id, attendance_id, lesson_id, student_id, old_status, new_status, reason, changed_by)
				SELECT $1, id, lesson_id, student_id, 'absent', 'excused', 'справка', $2 FROM attendance WHERE id=$3`, tenantID, erlanUser, lastAtt)
			var lessonID string
			tx.QueryRow(ctx, `SELECT lesson_id FROM attendance WHERE id=$1`, lastAtt).Scan(&lessonID)
			attendance.ApplyCharges(ctx, tx, lessonID, erlanUser)
			tx.Exec(ctx, `INSERT INTO audit_logs (tenant_id, user_id, entity_type, entity_id, action, old_values, new_values) VALUES ($1,$2,'student',$3,'attendance_changed','{"status":"absent"}','{"status":"excused","reason":"справка"}')`,
				tenantID, erlanUser, alim)
		}
		tx.Exec(ctx, `INSERT INTO audit_logs (tenant_id, user_id, entity_type, entity_id, action, old_values, new_values) VALUES ($1,$2,'student',$3,'update','{"cf.target_score":120}','{"cf.target_score":128}')`,
			tenantID, owner, alim)

		// лиды
		stages := []string{"new", "new", "new", "new", "new", "new", "contacted", "contacted", "contacted", "trial_scheduled", "trial_scheduled", "trial_done", "trial_done", "lost", "lost", "contacted", "new", "trial_done"}
		var trialLead string
		for i, stage := range stages {
			created := now.Add(-time.Duration(rnd.Intn(28*24)+30) * time.Hour)
			if stage == "new" && i < 5 {
				created = now.Add(-time.Duration(26+rnd.Intn(60)) * time.Hour) // без контакта больше 24 ч
			}
			if stage == "new" && i == 16 {
				created = now.Add(-2 * time.Hour)
			}
			var lastContact *time.Time
			if stage != "new" {
				lastContact = ptrTime(created.Add(3 * time.Hour))
			}
			var lost *string
			if stage == "lost" {
				lost = sp([]string{"Дорого", "Не подошло расписание"}[i%2])
			}
			course := []string{"ЕНТ Математика", "English B1", "IELTS Intensive", "Python Junior", "ЕНТ Физика", "English Kids"}[rnd.Intn(6)]
			var id string
			tx.QueryRow(ctx, `INSERT INTO leads (tenant_id, branch_id, full_name, contact_name, phone, course_id, source, responsible_id, stage, lost_reason, last_contact_at, age_or_grade, created_at, updated_at, utm)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13,$14) RETURNING id`,
				tenantID, b1, randomName(), femaleNames[rnd.Intn(len(femaleNames))]+" (мама)", randPhone(), courses[course],
				[]string{"Instagram", "2GIS", "Сайт", "Рекомендация", "Звонок", "WhatsApp"}[rnd.Intn(6)], salesUser, stage, lost, lastContact,
				[]string{"10 класс", "11 класс", "9 лет", "взрослый", "14 лет"}[rnd.Intn(5)], created, map[string]string{"utm_source": "instagram", "utm_campaign": "autumn"}).Scan(&id)
			if stage == "trial_scheduled" && trialLead == "" {
				trialLead = id
			}
		}
		// конвертированные лиды (Won) за месяц
		for i, sid := range allStudents[:20] {
			if i%4 != 0 {
				continue
			}
			var name, phone string
			tx.QueryRow(ctx, `SELECT full_name, phone FROM students WHERE id=$1`, sid).Scan(&name, &phone)
			var lid string
			tx.QueryRow(ctx, `INSERT INTO leads (tenant_id, branch_id, full_name, phone, source, responsible_id, stage, student_id, last_contact_at, created_at)
				VALUES ($1,$2,$3,$4,'Instagram',$5,'won',$6,NOW() - interval '20 days', NOW() - interval '25 days') RETURNING id`, tenantID, b1, name, phone, salesUser, sid).Scan(&lid)
			tx.Exec(ctx, `UPDATE students SET lead_id=$2 WHERE id=$1`, sid, lid)
		}
		// пробное занятие сегодня в Python Junior
		if trialLead != "" {
			var lessonID string
			tx.QueryRow(ctx, `SELECT l.id FROM lessons l WHERE l.group_id=$1 AND l.starts_at >= $2 AND l.starts_at < $3`, groups["Python Junior"], today, today.AddDate(0, 0, 1)).Scan(&lessonID)
			if lessonID != "" {
				tx.Exec(ctx, `INSERT INTO lesson_guests (tenant_id, lesson_id, lead_id, is_trial) VALUES ($1,$2,$3,true)`, tenantID, lessonID, trialLead)
			}
		}
		// активность сотрудников за 30 дней (для панели Super Admin)
		for d := 0; d < 30; d++ {
			for k := 0; k < 3+rnd.Intn(6); k++ {
				tx.Exec(ctx, `INSERT INTO audit_logs (tenant_id, user_id, entity_type, action, created_at) VALUES ($1,$2,'user','login',$3)`,
					tenantID, []string{owner, erlanUser, salesUser}[rnd.Intn(3)], now.AddDate(0, 0, -d).Add(-time.Duration(rnd.Intn(10))*time.Hour))
			}
		}
		tx.Exec(ctx, `UPDATE outbox_events SET processed_at=NOW()`) // исторические события сидов не рассылаем
		return nil
	})
}

// ---------------------------------------------------------------- другие организации (для Super Admin)

type orgDef struct {
	name, city, ctype, plan, status string
	branches, students             int
	createdMonth                   int
	lastActiveDays                 int
	markedPct, tgPct               int
	paidDays                       int // оплачено до: сегодня + N дней (отрицательное — просрочка)
	trialDays                      int
}

var otherOrgs = []orgDef{
	{"Orda English", "Астана", "language", "basic", "active", 1, 318, 7, 0, 95, 60, 10, 0},
	{"ЕНТ Мастер", "Шымкент", "ent", "business", "trial", 2, 141, 9, 1, 90, 0, 0, 4},
	{"CodeKids", "Алматы", "it", "starter", "past_due", 1, 74, 5, 9, 70, 40, -5, 0},
	{"Little Genius", "Караганда", "kids", "basic", "active", 2, 236, 2, 0, 61, 38, 16, 0},
	{"Сенім Академия", "Алматы", "ent", "pro", "active", 4, 1388, 1, 0, 97, 70, 59, 0},
	{"Speak Up Taraz", "Тараз", "language", "business", "trial", 1, 12, 10, 3, 80, 0, 0, 2},
	{"Ақыл Орталығы", "Актобе", "kids", "starter", "suspended", 1, 0, 4, 41, 0, 0, -31, 0},
}

func seedOrg(ctx context.Context, db *database.DB, hash string, o orgDef) error {
	loc, _ := time.LoadLocation("Asia/Almaty")
	now := time.Now().In(loc)
	var trial, paid *time.Time
	if o.trialDays != 0 {
		trial = ptrTime(now.AddDate(0, 0, o.trialDays))
	}
	if o.paidDays != 0 {
		paid = ptrTime(now.AddDate(0, 0, o.paidDays))
	}
	id, err := createTenant(ctx, db, o.name, o.ctype, o.city, o.plan, o.status, time.Date(2026, time.Month(o.createdMonth), 10, 10, 0, 0, 0, loc), trial, paid)
	if err != nil {
		return err
	}
	if o.name == "Orda English" {
		db.Sys.Exec(ctx, `UPDATE tenants SET over_limit_since=NOW() - interval '4 days' WHERE id=$1`, id)
	}
	return db.InTenant(ctx, id, func(tx pgx.Tx) error {
		ownerRole, err := organization.Bootstrap(ctx, tx, id, o.ctype)
		if err != nil {
			return err
		}
		var branch string
		for i := 0; i < o.branches; i++ {
			var b string
			tx.QueryRow(ctx, `INSERT INTO branches (tenant_id, name) VALUES ($1,$2) RETURNING id`, id, fmt.Sprintf("%s, филиал %d", o.city, i+1)).Scan(&b)
			if i == 0 {
				branch = b
			}
		}
		slug := strings.ToLower(strings.NewReplacer(" ", "", "Ақыл", "akyl", "Орталығы", "", "Сенім", "senim", "Академия", "", "ЕНТ", "ent", "Мастер", "master").Replace(o.name))
		var owner string
		ownerName := []string{"Жанар Оспанова", "Ермек Сагинтаев", "Динара Абдуллина", "Руслан Ким", "Сауле Ибраева", "Нурлан Ахметов", "Айжан Мукашева"}[rnd.Intn(7)]
		if err := tx.QueryRow(ctx, `INSERT INTO users (tenant_id, email, phone, full_name, password_hash, last_login_at) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
			id, "owner@"+slug+".kz", randPhone(), ownerName, hash, now.AddDate(0, 0, -o.lastActiveDays)).Scan(&owner); err != nil {
			return err
		}
		tx.Exec(ctx, `INSERT INTO user_roles (tenant_id, user_id, role_id) VALUES ($1,$2,$3)`, id, owner, ownerRole)
		if o.students > 0 {
			tx.Exec(ctx, `INSERT INTO students (tenant_id, branch_id, full_name, status, started_at)
				SELECT $1, $2, 'Ученик ' || g, 'active', CURRENT_DATE - (g % 60) FROM generate_series(1, $3) g`, id, branch, o.students)
			parents := o.students * o.tgPct / 100
			tx.Exec(ctx, `INSERT INTO parents (tenant_id, full_name, telegram_chat_id) SELECT $1, 'Родитель ' || g, 800000000 + g FROM generate_series(1, $2) g`, id, parents)
			tx.Exec(ctx, `INSERT INTO parents (tenant_id, full_name) SELECT $1, 'Родитель ' || g FROM generate_series(1, $2) g`, id, o.students-parents)
			var course, group string
			tx.QueryRow(ctx, `INSERT INTO courses (tenant_id, name, price_tiyn) VALUES ($1,'Основной курс',3000000) RETURNING id`, id).Scan(&course)
			tx.QueryRow(ctx, `INSERT INTO groups (tenant_id, branch_id, course_id, name) VALUES ($1,$2,$3,'Группа 1') RETURNING id`, id, branch, course).Scan(&group)
			for d := 1; d <= 14; d++ {
				day := now.AddDate(0, 0, -d)
				for k := 0; k < 4; k++ {
					st := "planned"
					if rnd.Intn(100) < o.markedPct {
						st = "done"
					}
					s := time.Date(day.Year(), day.Month(), day.Day(), 10+2*k, 0, 0, 0, loc)
					tx.Exec(ctx, `INSERT INTO lessons (tenant_id, branch_id, group_id, starts_at, ends_at, status) VALUES ($1,$2,$3,$4,$5,$6)`, id, branch, group, s, s.Add(90*time.Minute), st)
				}
			}
		}
		for d := o.lastActiveDays; d < 30; d++ {
			if o.status == "suspended" && d < 41 {
				continue
			}
			for k := 0; k < 1+rnd.Intn(8); k++ {
				tx.Exec(ctx, `INSERT INTO audit_logs (tenant_id, user_id, entity_type, action, created_at) VALUES ($1,$2,'user','login',$3)`, id, owner, now.AddDate(0, 0, -d).Add(-time.Duration(k)*time.Hour))
			}
		}
		if o.status == "active" {
			db.Sys.Exec(ctx, `INSERT INTO tenant_billing_payments (tenant_id, amount_tiyn, method, period_to) SELECT $1, price_tiyn, 'kaspi_invoice', CURRENT_DATE FROM subscription_plans WHERE code=$2`, id, o.plan)
		}
		return nil
	})
}

// ---------------------------------------------------------------- генераторы

var maleNames = []string{"Нурлан", "Ерлан", "Арман", "Даурен", "Ильяс", "Санжар", "Алихан", "Темирлан", "Ерасыл", "Арсен", "Бекзат", "Айдос", "Нуржан", "Мирас", "Адиль", "Алишер", "Данияр", "Тимур", "Руслан", "Азамат"}
var femaleNames = []string{"Аружан", "Мадина", "Томирис", "Дана", "Айым", "Жания", "Камила", "Айгерим", "Асель", "Динара", "Гульнара", "Сауле", "Алия", "Дильназ", "Амина", "Айша", "Жансая", "Карина", "Назерке", "Әсем"}
var surnames = []string{"Ахметов", "Беков", "Жумабаев", "Касымов", "Нурланов", "Оспанов", "Сагинтаев", "Тулегенов", "Утепов", "Ибраев", "Мукашев", "Абдуллин", "Серикбаев", "Есенов", "Жаксылыков", "Кенжебаев", "Муратов", "Садыков", "Тажибаев", "Шарипов", "Ким", "Пак", "Ли"}
var schools = []string{"НИШ ФМН", "РФМШ", "КТЛ", "Школа-лицей №165", "Гимназия №56", "Гимназия №159", "Школа №12", "Школа №134", "БИЛ"}
var topics = []string{"Квадратные уравнения", "Тригонометрия", "Present Perfect", "Reading: True/False/Not Given", "Циклы в Python", "Логарифмы", "Speaking Part 2", "Кинематика", "Функции и графики", "HTML и CSS"}

func randomName() string {
	s := surnames[rnd.Intn(len(surnames))]
	if rnd.Intn(2) == 0 {
		return s + " " + maleNames[rnd.Intn(len(maleNames))]
	}
	return femaleSurname(s) + " " + femaleNames[rnd.Intn(len(femaleNames))]
}

func femaleSurname(s string) string {
	switch {
	case strings.HasSuffix(s, "ов"), strings.HasSuffix(s, "ев"), strings.HasSuffix(s, "ин"):
		return s + "а"
	case strings.HasSuffix(s, "ова"), strings.HasSuffix(s, "ева"), strings.HasSuffix(s, "ина"):
		return s
	}
	return s
}

func maleSurname(s string) string {
	for _, suf := range []string{"ова", "ева", "ина"} {
		if strings.HasSuffix(s, suf) {
			return strings.TrimSuffix(s, "а")
		}
	}
	return s
}

func randPhone() string {
	return fmt.Sprintf("+77%s%07d", []string{"01", "02", "05", "07", "08", "47", "75", "77", "00"}[rnd.Intn(9)], rnd.Intn(10000000))
}

var lat = map[rune]string{'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ж': "zh", 'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o",
	'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "h", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'ы': "y", 'э': "e", 'ю': "yu", 'я': "ya", 'ә': "a", 'і': "i", 'ң': "n", 'ғ': "g", 'ү': "u", 'ұ': "u", 'қ': "k", 'ө': "o", 'һ': "h"}

func translitEmail(name string) string {
	parts := strings.Fields(strings.ToLower(name))
	conv := func(s string) string {
		var b strings.Builder
		for _, r := range s {
			b.WriteString(lat[r])
		}
		return b.String()
	}
	if len(parts) < 2 {
		return conv(parts[0]) + "@mail.kz"
	}
	return fmt.Sprintf("%s.%s%d@mail.kz", conv(parts[1]), conv(parts[0])[:1], rnd.Intn(90)+10)
}

// makeIIN генерирует корректный ИИН (дата рождения + контрольная цифра).
func makeIIN(birth time.Time) string {
	for {
		century := 5
		if birth.Year() < 2000 {
			century = 3
		}
		base := fmt.Sprintf("%02d%02d%02d%d%04d", birth.Year()%100, int(birth.Month()), birth.Day(), century+rnd.Intn(2), rnd.Intn(10000))
		n := make([]int, 11)
		for i, r := range base {
			n[i] = int(r - '0')
		}
		sum := 0
		for i := 0; i < 11; i++ {
			sum += n[i] * (i + 1)
		}
		c := sum % 11
		if c == 10 {
			sum = 0
			for i := 0; i < 11; i++ {
				sum += n[i] * ((i+2)%11 + 1)
			}
			c = sum % 11
			if c == 10 {
				continue
			}
		}
		return base + fmt.Sprint(c)
	}
}

func ip(i int) *int      { return &i }
func sp(s string) *string { return &s }
