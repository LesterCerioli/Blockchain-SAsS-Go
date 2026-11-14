package utils

import "github.com/google/uuid"

func NewUUID() uuid.UUID {
	return uuid.New()
}

func NewUUIDString() string {
	return uuid.NewString()
}

func ParseUUID(value string) (uuid.UUID, error) {
	return uuid.Parse(value)
}
