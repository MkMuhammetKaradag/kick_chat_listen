package bootstrap

import (
	"kick-chat/internal/config"
	"kick-chat/internal/initializer"
	"time"

	"github.com/google/uuid"
)

type PostgresRepository interface {
	InsertListener(username string, isActive bool, endTime *time.Time, duration int) (uuid.UUID, error)
	InsertUserListenerRequest(listenerID uuid.UUID, userID string, requestTime time.Time, endTime time.Time) error
	GetListenerByUsername(username string) (*struct { // Anonim struct kullanabiliriz veya Listener struct'ını import edebiliriz.
		ID       uuid.UUID
		Username string
		IsActive bool
		EndTime  *time.Time
		Duration int
	}, error)
	GetActiveListeners() ([]struct { // Anonim struct
		ID       uuid.UUID
		Username string
		IsActive bool
		EndTime  *time.Time
		Duration int
	}, error)
	GetUserRequestsForListener(listenerID uuid.UUID) ([]struct {
		UserID      string
		RequestTime time.Time
		EndTime     time.Time
	}, error)
	InsertMessage(listenerID uuid.UUID, senderUsername, content string, timestamp time.Time) error
}

func InitDatabase(config *config.Config) PostgresRepository {
	return initializer.InitDatabase(config)
}
