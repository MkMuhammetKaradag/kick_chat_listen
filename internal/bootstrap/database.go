package bootstrap

import (
	"context"
	"kick-chat/domain"
	"kick-chat/internal/config"
	"kick-chat/internal/initializer"
	"time"

	"github.com/google/uuid"
)

type PostgresRepository interface {
	InsertListener(username string, isActive bool, endTime *time.Time, duration int) (uuid.UUID, error)
	InsertUserListenerRequest(listenerID uuid.UUID, userID string, requestTime time.Time, endTime time.Time) error
	GetListenerByUsername(username string) (*struct {
		ID       uuid.UUID
		Username string
		IsActive bool
		EndTime  *time.Time
		Duration int
	}, error)
	GetActiveListeners() ([]struct {
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
	// GÜNCELLENDİ: InsertMessage fonksiyonu link bilgilerini de alıyor
	InsertMessage(listenerID uuid.UUID, senderUsername, content string, timestamp time.Time, hasLink bool, extractedLinks []string) error
	// YENİ: Eklenen fonksiyonlar
	UpdateListenerStatus(listenerID uuid.UUID, isActive bool) error
	UpdateListenerEndTime(listenerID uuid.UUID, endTime time.Time) error
	GetMessagesByListener(listenerID uuid.UUID, limit, offset int) ([]struct {
		ID               uuid.UUID
		SenderUsername   string
		Content          string
		MessageTimestamp time.Time
		HasLink          bool
		ExtractedLinks   []string
	}, error)

	SignUp(ctx context.Context, auth *domain.User) (uuid.UUID, error)
	SignIn(ctx context.Context, identifier, password string) (*domain.User, error)
}

func InitDatabase(config *config.Config) PostgresRepository {
	return initializer.InitDatabase(config)
}
