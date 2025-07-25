package bootstrap

import (
	"context"
	"database/sql"
	"kick-chat/domain"
	"kick-chat/internal/config"
	"kick-chat/internal/initializer"
	"time"

	"github.com/google/uuid"
)

type PostgresRepository interface {
	InsertListener(ctx context.Context, streamerUsername string, kickUserID *int, profilePic *string, userID uuid.UUID, newIsActive bool, newEndTime *time.Time, newDuration int) (uuid.UUID, error)

	InsertUserListenerRequest(listenerID uuid.UUID, userID uuid.UUID, requestTime time.Time, endTime time.Time) error

	GetStreamerByUsername(ctx context.Context, username string) (*struct {
		ID         uuid.UUID
		KickUserID sql.NullInt32
		ProfilePic sql.NullString
	}, error)

	GetListenerByStreamerIDAndUserID(ctx context.Context, streamerID, userID uuid.UUID) (*struct {
		ID         uuid.UUID
		StreamerID uuid.UUID // Now streamer_id
		UserID     uuid.UUID
		IsActive   bool
		EndTime    *time.Time
		Duration   int
	}, error)

	GetActiveListeners() ([]domain.ActiveListenerData, error)

	GetUserRequestsForListener(listenerID uuid.UUID) ([]struct {
		UserID      uuid.UUID
		RequestTime time.Time
		EndTime     time.Time
	}, error)
	// GÜNCELLENDİ: InsertMessage fonksiyonu link bilgilerini de alıyor
	InsertMessage(listenerID uuid.UUID, senderUsername, content string, timestamp time.Time, hasLink bool, extractedLinks []string) error
	// YENİ: Eklenen fonksiyonlar
	UpdateListenerStatus(listenerID uuid.UUID, isActive bool) error
	UpdateListenerEndTime(ctx context.Context, listenerID uuid.UUID, endTime time.Time) error
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
