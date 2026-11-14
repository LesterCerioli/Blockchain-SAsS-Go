package ports

import (
	"testing"
	"time"
)

func TestNotificationDataString(t *testing.T) {
	notification := Notification{}
	if got := notification.DataString("id"); got != "" {
		t.Fatalf("nil data = %q", got)
	}

	notification.Data = map[string]interface{}{
		"id":    "abc",
		"count": 42,
		"nil":   nil,
	}
	if got := notification.DataString("id"); got != "abc" {
		t.Fatalf("string value = %q", got)
	}
	if got := notification.DataString("count"); got != "42" {
		t.Fatalf("int value = %q", got)
	}
	if got := notification.DataString("nil"); got != "" {
		t.Fatalf("nil value = %q", got)
	}
	if got := notification.DataString("missing"); got != "" {
		t.Fatalf("missing value = %q", got)
	}
}

func TestSystemClockAndDefaults(t *testing.T) {
	now := (SystemClock{}).Now()
	if now.IsZero() || now.Location() != time.UTC {
		t.Fatalf("now = %s", now)
	}
	pagination := Pagination{}
	if pagination.Limit != 0 || pagination.Offset != 0 {
		t.Fatalf("pagination = %+v", pagination)
	}
	filter := PaymentFilter{}
	if filter.MerchantID.String() != "00000000-0000-0000-0000-000000000000" {
		t.Fatalf("filter = %+v", filter)
	}
}
