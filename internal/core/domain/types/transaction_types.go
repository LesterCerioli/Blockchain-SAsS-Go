package types

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type TransactionStatus string

const (
	StatusPending     TransactionStatus = "pending"
	StatusConfirmed   TransactionStatus = "confirmed"
	StatusFailed      TransactionStatus = "failed"
	StatusUnconfirmed TransactionStatus = "unconfirmed"
	StatusDropped     TransactionStatus = "dropped"
	StatusReplaced    TransactionStatus = "replaced"
	StatusExpired     TransactionStatus = "expired"
	StatusMempool     TransactionStatus = "mempool"
)

type TransactionType string

const (
	TxTypePayment    TransactionType = "payment"
	TxTypeContract   TransactionType = "contract"
	TxTypeToken      TransactionType = "token"
	TxTypeDeploy     TransactionType = "deploy"
	TxTypeStaking    TransactionType = "staking"
	TxTypeGovernance TransactionType = "governance"
)

type TransactionPriority string

const (
	PriorityLow    TransactionPriority = "low"
	PriorityMedium TransactionPriority = "medium"
	PriorityHigh   TransactionPriority = "high"
)

type TransactionDirection string

const (
	DirectionIncoming TransactionDirection = "incoming"
	DirectionOutgoing TransactionDirection = "outgoing"
	DirectionInternal TransactionDirection = "internal"
)

type TransactionMetadata struct {
	ID            uuid.UUID            `json:"id" gorm:"type:uuid;default:uuid_generate_v4();primaryKey"`
	TxHash        string               `json:"tx_hash"`
	Status        TransactionStatus    `json:"status"`
	Type          TransactionType      `json:"type"`
	Priority      TransactionPriority  `json:"priority"`
	Direction     TransactionDirection `json:"direction"`
	Timestamp     time.Time            `json:"timestamp"`
	Confirmations int                  `json:"confirmations"`
	Amount        float64              `json:"amount"`
	Fee           float64              `json:"fee"`
	Sender        string               `json:"sender"`
	Receiver      string               `json:"receiver"`
	Memo          string               `json:"memo,omitempty"`
	Tags          []string             `json:"tags,omitempty"`
}

type TransactionConfirmation struct {
	BlockHeight   int64     `json:"block_height"`
	BlockHash     string    `json:"block_hash"`
	BlockTime     time.Time `json:"block_time"`
	Confirmations int       `json:"confirmations"`
	Finalized     bool      `json:"finalized"`
}

func (s TransactionStatus) IsFinalized() bool {
	return s == StatusConfirmed || s == StatusFailed ||
		s == StatusDropped || s == StatusReplaced ||
		s == StatusExpired
}

func (s TransactionStatus) IsSuccessful() bool {
	return s == StatusConfirmed
}

func (s TransactionStatus) RequiresConfirmation() bool {
	return s == StatusPending || s == StatusUnconfirmed ||
		s == StatusMempool
}

func (s TransactionStatus) CanRetry() bool {
	return s == StatusFailed || s == StatusDropped ||
		s == StatusExpired
}

func (s TransactionStatus) String() string {
	return string(s)
}

func (s TransactionStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(s))
}

func (s *TransactionStatus) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}

	*s = TransactionStatus(str)
	if !s.IsValid() {
		return fmt.Errorf("invalid transaction status: %s", str)
	}
	return nil
}

func (s TransactionStatus) IsValid() bool {
	switch s {
	case StatusPending, StatusConfirmed, StatusFailed,
		StatusUnconfirmed, StatusDropped, StatusReplaced,
		StatusExpired, StatusMempool:
		return true
	default:
		return false
	}
}

func ParseTransactionStatus(status string) (TransactionStatus, error) {
	ts := TransactionStatus(status)
	if ts.IsValid() {
		return ts, nil
	}
	return "", fmt.Errorf("invalid transaction status: %s", status)
}

func GetStatusFromConfirmations(confirmations, requiredConfirmations int) TransactionStatus {
	if confirmations >= requiredConfirmations {
		return StatusConfirmed
	} else if confirmations > 0 {
		return StatusPending
	}
	return StatusUnconfirmed
}
