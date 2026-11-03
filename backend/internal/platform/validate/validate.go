// Пакет validate: нормализация телефонов (E.164), проверка ИИН, email.
package validate

import (
	"net/mail"
	"strings"
	"unicode"
)

// Phone приводит казахстанские/российские номера к E.164: "8 701 …", "+7 (701) …", "7701…" → "+77011234567".
// Возвращает ok=false, если номер не распознан.
func Phone(in string) (string, bool) {
	in = strings.TrimSpace(in)
	if in == "" {
		return "", true
	}
	var d strings.Builder
	for _, r := range in {
		if unicode.IsDigit(r) {
			d.WriteRune(r)
		}
	}
	digits := d.String()
	switch {
	case len(digits) == 11 && (digits[0] == '8' || digits[0] == '7'):
		digits = "7" + digits[1:]
	case len(digits) == 10:
		digits = "7" + digits
	case strings.HasPrefix(in, "+") && len(digits) >= 8 && len(digits) <= 15:
		return "+" + digits, true
	default:
		return "", false
	}
	return "+" + digits, true
}

// FormatPhone: +77012451890 → +7 701 245-18-90.
func FormatPhone(e164 string) string {
	if len(e164) != 12 || !strings.HasPrefix(e164, "+7") {
		return e164
	}
	d := e164[2:]
	return "+7 " + d[0:3] + " " + d[3:6] + "-" + d[6:8] + "-" + d[8:10]
}

// IIN проверяет 12-значный ИИН РК с контрольной цифрой.
func IIN(s string) bool {
	if len(s) != 12 {
		return false
	}
	n := make([]int, 12)
	for i, r := range s {
		if r < '0' || r > '9' {
			return false
		}
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
			w := (i+2)%11 + 1 // 3,4,…,11,1,2
			sum += n[i] * w
		}
		c = sum % 11
		if c == 10 {
			return false
		}
	}
	return c == n[11]
}

func MaskIIN(s string) string {
	if len(s) < 4 {
		return s
	}
	return "•••• •••• " + s[len(s)-4:]
}

func Email(s string) bool {
	if s == "" {
		return true
	}
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s
}
