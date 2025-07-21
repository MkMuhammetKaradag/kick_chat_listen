package postgres

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
)

func (r *Repository) InsertListener(username string, isActive bool, endTime *time.Time, duration int) (uuid.UUID, error) {
	var listenerID uuid.UUID
	query := `INSERT INTO listeners (username, is_active, end_time, duration) VALUES ($1, $2, $3, $4) RETURNING id;`
	err := r.db.QueryRow(query, username, isActive, endTime, duration).Scan(&listenerID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("listener eklenirken hata: %w", err)
	}
	return listenerID, nil
}

func (r *Repository) InsertUserListenerRequest(listenerID uuid.UUID, userID string, requestTime time.Time, endTime time.Time) error {
	query := `INSERT INTO user_listener_requests (listener_id, user_id, request_time, end_time) VALUES ($1, $2, $3, $4);`
	_, err := r.db.Exec(query, listenerID, userID, requestTime, endTime)
	if err != nil {
		return fmt.Errorf("user listener request eklenirken hata: %w", err)
	}
	return nil
}

func (r *Repository) GetListenerByUsername(username string) (*struct { // Anonim struct kullanabiliriz veya Listener struct'ını import edebiliriz.
	ID       uuid.UUID
	Username string
	IsActive bool
	EndTime  *time.Time
	Duration int
}, error) {
	var listenerData struct {
		ID       uuid.UUID
		Username string
		IsActive bool
		EndTime  *time.Time
		Duration int
	}
	query := `SELECT id, username, is_active, end_time, duration FROM listeners WHERE username = $1;`
	row := r.db.QueryRow(query, username)
	err := row.Scan(&listenerData.ID, &listenerData.Username, &listenerData.IsActive, &listenerData.EndTime, &listenerData.Duration)
	if err == sql.ErrNoRows {
		return nil, nil // Bulunamadı
	} else if err != nil {
		return nil, fmt.Errorf("listener getirilirken hata: %w", err)
	}
	return &listenerData, nil
}

// Örnek: Aktif listener'ları alma fonksiyonu
func (r *Repository) GetActiveListeners() ([]struct { // Anonim struct
	ID       uuid.UUID
	Username string
	IsActive bool
	EndTime  *time.Time
	Duration int
}, error) {
	var listeners []struct {
		ID       uuid.UUID
		Username string
		IsActive bool
		EndTime  *time.Time
		Duration int
	}
	now := time.Now()
	// Aktif olan ve süresi geçmemiş listener'ları getir
	query := `SELECT id, username, is_active, end_time, duration FROM listeners WHERE is_active = true AND (end_time IS NULL OR end_time > $1);`
	rows, err := r.db.Query(query, now)
	if err != nil {
		return nil, fmt.Errorf("aktif listener'lar getirilirken hata: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var l struct {
			ID       uuid.UUID
			Username string
			IsActive bool
			EndTime  *time.Time
			Duration int
		}
		if err := rows.Scan(&l.ID, &l.Username, &l.IsActive, &l.EndTime, &l.Duration); err != nil {
			log.Printf("Satır okunurken hata: %v", err)
			continue
		}
		listeners = append(listeners, l)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("satır döngüsü hatası: %w", err)
	}
	return listeners, nil
}

// Örnek: Belirli bir listener'a ait user listener request'lerini alma fonksiyonu
func (r *Repository) GetUserRequestsForListener(listenerID uuid.UUID) ([]struct {
	UserID      string
	RequestTime time.Time
	EndTime     time.Time
}, error) {
	var requests []struct {
		UserID      string
		RequestTime time.Time
		EndTime     time.Time
	}
	now := time.Now()
	query := `SELECT user_id, request_time, end_time FROM user_listener_requests WHERE listener_id = $1 AND end_time > $2;`
	rows, err := r.db.Query(query, listenerID, now)
	if err != nil {
		return nil, fmt.Errorf("user listener request'leri getirilirken hata: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var req struct {
			UserID      string
			RequestTime time.Time
			EndTime     time.Time
		}
		if err := rows.Scan(&req.UserID, &req.RequestTime, &req.EndTime); err != nil {
			log.Printf("User request satırı okunurken hata: %v", err)
			continue
		}
		requests = append(requests, req)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("user request satır döngüsü hatası: %w", err)
	}
	return requests, nil
}
func (r *Repository) InsertMessage(listenerID uuid.UUID, senderUsername, content string, timestamp time.Time) error {
	query := `INSERT INTO messages (listener_id, sender_username, content, message_timestamp) VALUES ($1, $2, $3, $4);`
	_, err := r.db.Exec(query, listenerID, senderUsername, content, timestamp)
	if err != nil {
		return fmt.Errorf("mesaj kaydedilirken hata: %w", err)
	}
	return nil
}
