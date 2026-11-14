package utils

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAPIKeyLifecycle(t *testing.T) {
	key, hash, prefix, err := GenerateAPIKey("test")
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if key == "" || hash == "" || prefix == "" {
		t.Fatal("expected non-empty key, hash and prefix")
	}
	if HashAPIKey(key) != hash {
		t.Fatal("hash mismatch")
	}
	if !SecureCompare(prefix, key[:len(prefix)]) {
		t.Fatal("prefix should match key prefix")
	}
	if !SecureCompare(key, key) {
		t.Fatal("constant time compare should match identical values")
	}
	if SecureCompare(key, key+"x") {
		t.Fatal("different values must not match")
	}
}

func TestWebhookSignature(t *testing.T) {
	secret := "top-secret"
	body := []byte(`{"event":"payment.succeeded"}`)
	timestamp := "1700000000"

	signature := SignWebhook(secret, timestamp, body)
	if signature == "" {
		t.Fatal("expected signature")
	}
	if !VerifyWebhookSignature(secret, timestamp, body, signature) {
		t.Fatal("valid signature rejected")
	}
	if !VerifyWebhookSignature(secret, timestamp, body, "sha256="+signature) {
		t.Fatal("sha256= prefixed signature rejected")
	}
	if VerifyWebhookSignature(secret, timestamp, []byte("tampered"), signature) {
		t.Fatal("tampered body accepted")
	}
	if VerifyWebhookSignature("other-secret", timestamp, body, signature) {
		t.Fatal("wrong secret accepted")
	}

	now := time.Unix(1700000000, 0).UTC()
	if err := ValidateWebhookTimestamp(timestamp, now, DefaultWebhookTimestampTolerance); err != nil {
		t.Fatalf("fresh timestamp rejected: %v", err)
	}
	if err := ValidateWebhookTimestamp(timestamp, now.Add(10*time.Minute), DefaultWebhookTimestampTolerance); err == nil {
		t.Fatal("stale timestamp accepted")
	}
	if err := ValidateWebhookTimestamp(timestamp, now.Add(-10*time.Minute), DefaultWebhookTimestampTolerance); err == nil {
		t.Fatal("future timestamp accepted")
	}
	if err := ValidateWebhookTimestamp("not-a-timestamp", now, DefaultWebhookTimestampTolerance); err == nil {
		t.Fatal("malformed timestamp accepted")
	}
}

func TestJWTRoundTrip(t *testing.T) {
	secret := "a-very-long-test-secret-for-hmac-signing"
	merchantID := uuid.New()
	now := time.Now().UTC()

	token, expires, err := CreateJWT(secret, "issuer", []string{"audience"}, merchantID, "merchant", time.Hour, now)
	if err != nil {
		t.Fatalf("CreateJWT: %v", err)
	}
	if !expires.After(now) {
		t.Fatal("expiry must be in the future")
	}

	claims, err := ParseJWT(secret, token)
	if err != nil {
		t.Fatalf("ParseJWT: %v", err)
	}
	if claims.MerchantID != merchantID.String() {
		t.Fatalf("merchant id = %s, want %s", claims.MerchantID, merchantID)
	}
	if claims.Issuer != "issuer" {
		t.Fatalf("issuer = %s", claims.Issuer)
	}
	if _, err := ParseJWT("wrong-secret", token); err == nil {
		t.Fatal("token with wrong secret must fail")
	}
}

func TestValidationHelpers(t *testing.T) {
	if !ValidateEmail("merchant@example.com") {
		t.Fatal("valid email rejected")
	}
	if ValidateEmail("not-an-email") {
		t.Fatal("invalid email accepted")
	}
	if err := ValidateAmount("1.25"); err != nil {
		t.Fatalf("valid amount rejected: %v", err)
	}
	for _, invalid := range []string{"", "0", "-1", "abc"} {
		if err := ValidateAmount(invalid); err == nil {
			t.Fatalf("invalid amount %q accepted", invalid)
		}
	}
	if got := NormalizeCurrency(" eth "); got != "ETH" {
		t.Fatalf("NormalizeCurrency = %q", got)
	}
}
