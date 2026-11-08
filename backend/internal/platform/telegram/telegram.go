// Пакет telegram — вызовы Bot API и выбор бота организации: собственный бот школы или общий бот платформы.
package telegram

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var client = &http.Client{Timeout: 40 * time.Second}

// ErrNetwork — Telegram недоступен; такую отправку имеет смысл повторить позже.
var ErrNetwork = errors.New("telegram: сеть недоступна")

// Call вызывает метод Bot API; out заполняется полем result ответа.
func Call(ctx context.Context, token, method string, body, out any) error {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+token+"/"+method, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		// в тексте ошибки net/http есть URL с токеном — не пробрасываем его в логи
		return ErrNetwork
	}
	defer resp.Body.Close()
	var res struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return err
	}
	if !res.OK {
		return errors.New(res.Description)
	}
	if out != nil {
		return json.Unmarshal(res.Result, out)
	}
	return nil
}

// BotID — числовой ID бота: часть токена до двоеточия.
func BotID(token string) int64 {
	id, _ := strconv.ParseInt(strings.SplitN(token, ":", 2)[0], 10, 64)
	return id
}

type Bot struct {
	Token    string
	ID       int64
	Username string
	Own      bool // собственный бот школы, а не общий бот платформы
}

// Platform — общий бот из конфигурации (TELEGRAM_BOT_TOKEN); используется школами без своего бота.
type Platform struct {
	Token    string
	Username string
}

// For возвращает бота организации; ok=false — Telegram для школы недоступен.
func (p Platform) For(ctx context.Context, sys *pgxpool.Pool, tenantID string) (Bot, bool) {
	var b Bot
	err := sys.QueryRow(ctx, `SELECT token, bot_id, username FROM tenant_bots WHERE tenant_id=$1 AND enabled`, tenantID).Scan(&b.Token, &b.ID, &b.Username)
	if err == nil {
		b.Own = true
		return b, true
	}
	if !errors.Is(err, pgx.ErrNoRows) || p.Token == "" {
		return Bot{}, false
	}
	return Bot{Token: p.Token, ID: BotID(p.Token), Username: p.Username}, true
}

// Username бота организации внутри транзакции под RLS (для ссылок-приглашений).
func Username(ctx context.Context, tx pgx.Tx, platformName string) string {
	var name string
	if err := tx.QueryRow(ctx, `SELECT username FROM tenant_bots WHERE tenant_id = app_current_tenant() AND enabled`).Scan(&name); err != nil {
		return platformName
	}
	return name
}

// MiniAppURL — адрес кабинета организации; открывается кнопкой меню бота.
func MiniAppURL(baseURL, tenantID string) string {
	return strings.TrimRight(baseURL, "/") + "/app?t=" + tenantID
}

// VerifyInitData проверяет подпись initData Telegram Mini App токеном бота и возвращает user.id.
func VerifyInitData(initData, botToken string, maxAge time.Duration) (int64, error) {
	vals, err := url.ParseQuery(initData)
	if err != nil {
		return 0, err
	}
	hash := vals.Get("hash")
	vals.Del("hash")
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + vals.Get(k)
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(botToken))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(parts, "\n")))
	if !hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(hash)) {
		return 0, errors.New("bad hash")
	}
	authDate, _ := strconv.ParseInt(vals.Get("auth_date"), 10, 64)
	if time.Since(time.Unix(authDate, 0)) > maxAge {
		return 0, errors.New("expired")
	}
	var user struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(vals.Get("user")), &user); err != nil || user.ID == 0 {
		return 0, errors.New("no user")
	}
	return user.ID, nil
}

// OpenButton — inline-кнопка, открывающая кабинет как Mini App.
func OpenButton(url string) map[string]any {
	return map[string]any{"inline_keyboard": [][]map[string]any{{{"text": "Открыть кабинет", "web_app": map[string]string{"url": url}}}}}
}
