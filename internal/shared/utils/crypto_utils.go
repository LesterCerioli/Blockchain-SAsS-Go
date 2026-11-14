package utils

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const APIKeyPrefix = "pk"

func GenerateAPIKey(env string) (key, hash, displayPrefix string, err error) {
	if env == "" {
		env = "test"
	}
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", "", fmt.Errorf("generate api key: %w", err)
	}
	raw := base64.RawURLEncoding.EncodeToString(buf)
	key = fmt.Sprintf("%s_%s_%s", APIKeyPrefix, env, raw)
	hash = HashAPIKey(key)
	if len(key) > 12 {
		displayPrefix = key[:12]
	} else {
		displayPrefix = key
	}
	return key, hash, displayPrefix, nil
}

func HashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func SecureCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func RandomToken(bytes int) (string, error) {
	if bytes <= 0 {
		bytes = 16
	}
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func SignWebhook(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func VerifyWebhookSignature(secret, timestamp string, body []byte, signature string) bool {
	expected := SignWebhook(secret, timestamp, body)
	signature = strings.TrimSpace(strings.TrimPrefix(signature, "sha256="))
	return subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) == 1
}

const DefaultWebhookTimestampTolerance = 5 * time.Minute

func ValidateWebhookTimestamp(timestamp string, now time.Time, tolerance time.Duration) error {
	seconds, err := strconv.ParseInt(strings.TrimSpace(timestamp), 10, 64)
	if err != nil {
		return fmt.Errorf("invalid webhook timestamp")
	}
	delta := now.UTC().Sub(time.Unix(seconds, 0).UTC())
	if delta > tolerance || delta < -tolerance {
		return fmt.Errorf("webhook timestamp outside tolerance")
	}
	return nil
}

func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
