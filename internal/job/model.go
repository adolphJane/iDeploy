package job

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCanceling Status = "canceling"
	StatusCanceled  Status = "canceled"
)

type Job struct {
	ID              string
	Type            string
	LockKey         string
	Payload         string
	Status          Status
	Progress        int
	Message         string
	Error           string
	Attempt         int
	CancelRequested bool
	CreatedAt       time.Time
	StartedAt       *time.Time
	FinishedAt      *time.Time
	LeaseUntil      *time.Time
	UpdatedAt       time.Time
}

type NewJob struct {
	Type    string
	LockKey string
	Payload string
}

func newID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate job id: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
