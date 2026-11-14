package utils

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type JWTClaims struct {
	Subject    string   `json:"sub"`
	MerchantID string   `json:"merchant_id"`
	Issuer     string   `json:"iss"`
	Audience   []string `json:"aud,omitempty"`
	IssuedAt   int64    `json:"iat"`
	ExpiresAt  int64    `json:"exp"`
	Role       string   `json:"role,omitempty"`
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

func CreateJWT(secret, issuer string, audience []string, merchantID uuid.UUID, role string, ttl time.Duration, now time.Time) (string, time.Time, error) {
	if len(secret) < 16 {
		return "", time.Time{}, errors.New("jwt secret must be at least 16 characters")
	}
	expires := now.UTC().Add(ttl)
	claims := JWTClaims{
		Subject:    merchantID.String(),
		MerchantID: merchantID.String(),
		Issuer:     issuer,
		Audience:   audience,
		IssuedAt:   now.UTC().Unix(),
		ExpiresAt:  expires.Unix(),
		Role:       role,
	}

	headerJSON, _ := json.Marshal(jwtHeader{Alg: "HS256", Typ: "JWT"})
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, err
	}

	header := base64.RawURLEncoding.EncodeToString(headerJSON)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	signingInput := header + "." + payload
	signature := signJWT(secret, signingInput)
	return signingInput + "." + signature, expires, nil
}

func ParseJWT(secret, token string) (*JWTClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed jwt")
	}
	signingInput := parts[0] + "." + parts[1]
	expected := signJWT(secret, signingInput)
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return nil, errors.New("invalid jwt signature")
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("invalid jwt header")
	}
	var header jwtHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil || header.Alg != "HS256" {
		return nil, errors.New("unsupported jwt algorithm")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("invalid jwt payload")
	}
	var claims JWTClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, errors.New("invalid jwt claims")
	}
	if claims.ExpiresAt <= time.Now().UTC().Unix() {
		return nil, fmt.Errorf("jwt expired at %s", time.Unix(claims.ExpiresAt, 0).UTC().Format(time.RFC3339))
	}
	return &claims, nil
}

func signJWT(secret, signingInput string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
