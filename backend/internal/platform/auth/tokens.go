package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/argon2"
)

// ---------------------------------------------------------------- пароли (Argon2id)

const (
	argonTime    = 2
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
)

func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func CheckPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// ---------------------------------------------------------------- JWT access token

const AccessTTL = 15 * time.Minute
const RefreshTTL = 30 * 24 * time.Hour

type Claims struct {
	TenantID string `json:"tid,omitempty"`
	Platform bool   `json:"pa,omitempty"`
	jwt.RegisteredClaims
}

func IssueAccess(secret, userID, tenantID string, platform bool) (string, error) {
	now := time.Now()
	c := Claims{TenantID: tenantID, Platform: platform, RegisteredClaims: jwt.RegisteredClaims{
		Subject:   userID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(AccessTTL)),
		Issuer:    "emp",
	}}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(secret))
}

func ParseAccess(secret, token string) (*Claims, error) {
	c := &Claims{}
	t, err := jwt.ParseWithClaims(token, c, func(t *jwt.Token) (any, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("emp"))
	if err != nil || !t.Valid {
		return nil, errors.New("invalid token")
	}
	return c, nil
}

// ---------------------------------------------------------------- refresh token

func NewRefreshToken() (token string, hash []byte) {
	b := make([]byte, 32)
	rand.Read(b)
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashRefresh(token)
}

func HashRefresh(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

func RandomCode(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return strings.ToLower(base64.RawURLEncoding.EncodeToString(b))[:n]
}
