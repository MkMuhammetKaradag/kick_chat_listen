// Package session, oturum yönetimi işlemlerini içerir
package session

import (
	"context"       // Context işlemleri için
	"encoding/json" // JSON işlemleri için
	"fmt"           // String formatlama için
	"kick-chat/domain"
	"time" // Zaman işlemleri için

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9" // Redis istemcisi
)

// SessionManager, oturum yönetimi işlemlerini gerçekleştiren yapı
type SessionManager struct {
	client *redis.Client // Redis istemcisi
}
type UserLogoutNotification struct {
	UserID    uuid.UUID `json:"user_id"`
	Timestamp int64     `json:"timestamp"`
}

// NewSessionManager, yeni bir SessionManager örneği oluşturur
// redisAddr: Redis sunucusunun adresi
func NewSessionManager(redisAddr string, password string, db int) (*SessionManager, error) {
	// Redis istemcisini oluştur
	client := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: password, // Redis şifresi (varsayılan: boş)
		DB:       db,       // Redis veritabanı numarası (varsayılan: 0)
	})

	// Redis bağlantısını test et
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &SessionManager{
		client: client,
	}, nil
}
func (sm *SessionManager) GetRedisClient() *redis.Client {
	return sm.client
}

// CreateSession, yeni bir oturum oluşturur
// ctx: Context
// userID: Kullanıcı ID'si
// token: Oturum token'ı
// userData: Kullanıcı bilgileri
// duration: Oturum süresi
func (sm *SessionManager) CreateSession(ctx context.Context, userID, token string, userData map[string]string, duration time.Duration) error {
	// Kullanıcı verilerini JSON'a dönüştür
	jsonData, err := json.Marshal(userData)
	if err != nil {
		return err
	}

	// Redis pipeline oluştur (birden fazla işlemi tek seferde yapmak için)
	pipe := sm.client.Pipeline()
	// Token'ı ve kullanıcı verilerini kaydet
	pipe.Set(ctx, token, jsonData, duration)
	// Kullanıcının oturum token'ını set'e ekle
	pipe.SAdd(ctx, "user_sessions:"+userID, token)
	// İşlemleri yürüt
	_, err = pipe.Exec(ctx)
	return err
}

// GetSession, belirtilen token'a ait oturum bilgilerini getirir
// ctx: Context
// token: Oturum token'ı
func (sm *SessionManager) GetSession(ctx context.Context, token string) (*domain.Session, error) {
	// Token'a ait verileri Redis'ten al
	sessionJSON, err := sm.client.Get(ctx, token).Bytes()
	if err != nil {
		return nil, err
	}

	// JSON verilerini Session yapısına dönüştür
	var session domain.Session
	if err := json.Unmarshal(sessionJSON, &session); err != nil {

		return nil, err
	}

	return &session, nil
}

// DeleteSession, belirtilen token'a ait oturumu siler
// ctx: Context
// token: Oturum token'ı
func (sm *SessionManager) DeleteSession(ctx context.Context, token string) error {
	// Token'a ait verileri Redis'ten al
	val, err := sm.client.Get(ctx, token).Result()
	if err == redis.Nil {
		// Token zaten yok, yapılacak iş yok
		return nil
	}
	if err != nil {
		return err
	}

	// JSON verilerini Session yapısına dönüştür
	var sess domain.Session
	if err := json.Unmarshal([]byte(val), &sess); err != nil {
		return err
	}

	// Redis pipeline oluştur
	pipe := sm.client.Pipeline()
	// Token'ı sil
	pipe.Del(ctx, token)
	// Kullanıcının oturum token'ını set'ten kaldır
	pipe.SRem(ctx, "user_sessions:"+sess.UserID, token)
	// İşlemleri yürüt
	_, err = pipe.Exec(ctx)
	return err
}

// DeleteAllUserSessions, kullanıcının tüm oturumlarını siler
// ctx: Context
// userID: Kullanıcı ID'si
func (sm *SessionManager) DeleteAllUserSessions(ctx context.Context, userID string) error {
	sessionSetKey := "user_sessions:" + userID
	fmt.Println("Deleting all sessions for user:", userID)
	// Kullanıcının tüm oturum tokenlarını al
	tokens, err := sm.client.SMembers(ctx, sessionSetKey).Result()
	if err != nil {
		return fmt.Errorf("failed to get session tokens for user %s: %w", userID, err)
	}

	if len(tokens) == 0 {
		// Silinecek bir şey yok
		return nil
	}

	// Redis pipeline oluştur
	pipe := sm.client.Pipeline()

	// Her token'ı sil
	for _, token := range tokens {
		pipe.Del(ctx, token)
	}

	// Token set'ini de sil
	pipe.Del(ctx, sessionSetKey)

	// İşlemleri yürüt
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete sessions for user %s: %w", userID, err)
	}

	return nil
}

// IsValid, token'ın geçerli olup olmadığını kontrol eder
// ctx: Context
// token: Oturum token'ı
func (sm *SessionManager) IsValid(ctx context.Context, token string) bool {
	// Token'ın Redis'te var olup olmadığını kontrol et
	exists, err := sm.client.Exists(ctx, token).Result()
	if err != nil {
		return false
	}
	return exists == 1
}
