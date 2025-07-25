package domain

import (
	"time"

	"github.com/google/uuid"
)

type ActiveListenerData struct {
	ID               uuid.UUID
	StreamerID       uuid.UUID // New: now includes the streamer's UUID
	StreamerUsername string
	UserID           uuid.UUID
	IsActive         bool
	EndTime          *time.Time
	Duration         int
}
