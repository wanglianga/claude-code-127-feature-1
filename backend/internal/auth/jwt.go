package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type Claims struct {
	UserID int64  `json:"uid"`
	Role   string `json:"role"`
	Name   string `json:"name"`
	Exp    int64  `json:"exp"`
}

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// 轻量自实现 JWT(HS256)，避免额外依赖
func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func IssueToken(secret string, c Claims) (string, error) {
	c.Exp = time.Now().Add(12 * time.Hour).Unix()
	header := `{"alg":"HS256","typ":"JWT"}`
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	signingInput := b64([]byte(header)) + "." + b64(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	return signingInput + "." + b64(mac.Sum(nil)), nil
}

func ParseToken(secret, token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, errors.New("invalid token")
	}
	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	if !hmac.Equal([]byte(parts[2]), []byte(b64(mac.Sum(nil)))) {
		return Claims{}, errors.New("bad signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, err
	}
	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return Claims{}, err
	}
	if time.Now().Unix() > c.Exp {
		return Claims{}, errors.New("token expired")
	}
	return c, nil
}
