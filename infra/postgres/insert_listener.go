package postgres

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
)

func (r *Repository) InsertListener(streamerUsername string, userID uuid.UUID, isActive bool, endTime *time.Time, duration int) (uuid.UUID, error) {
	var listenerID uuid.UUID
	query := `INSERT INTO listeners (streamer_username, user_id, is_active, end_time, duration) VALUES ($1, $2, $3, $4, $5) RETURNING id;`
	err := r.db.QueryRow(query, streamerUsername, userID, isActive, endTime, duration).Scan(&listenerID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("listener eklenirken hata: %w", err)
	}
	return listenerID, nil
}

func (r *Repository) InsertUserListenerRequest(listenerID uuid.UUID, userID uuid.UUID, requestTime time.Time, endTime time.Time) error {
	query := `INSERT INTO user_listener_requests (listener_id, user_id, request_time, end_time) VALUES ($1, $2, $3, $4);`
	_, err := r.db.Exec(query, listenerID, userID, requestTime, endTime)
	if err != nil {
		return fmt.Errorf("user listener request eklenirken hata: %w", err)
	}
	return nil
}

func (r *Repository) GetListenerByStreamerUsername(streamerUsername string) (*struct {
	ID               uuid.UUID
	StreamerUsername string
	UserID           uuid.UUID
	IsActive         bool
	EndTime          *time.Time
	Duration         int
}, error) {
	var listenerData struct {
		ID               uuid.UUID
		StreamerUsername string
		UserID           uuid.UUID
		IsActive         bool
		EndTime          *time.Time
		Duration         int
	}
	// Yeni sorgu: streamer_username'e göre arama yapıyor ve yeni sütunları seçiyor.
	query := `SELECT id, streamer_username, user_id, is_active, end_time, duration FROM listeners WHERE streamer_username = $1;`
	row := r.db.QueryRow(query, streamerUsername)
	err := row.Scan(&listenerData.ID, &listenerData.StreamerUsername, &listenerData.UserID, &listenerData.IsActive, &listenerData.EndTime, &listenerData.Duration)
	if err == sql.ErrNoRows {
		return nil, nil // Bulunamadı
	} else if err != nil {
		return nil, fmt.Errorf("listener getirilirken hata: %w", err)
	}
	return &listenerData, nil
}

func (r *Repository) GetActiveListeners() ([]struct {
	ID               uuid.UUID
	StreamerUsername string
	UserID           uuid.UUID
	IsActive         bool
	EndTime          *time.Time
	Duration         int
}, error) {
	var listeners []struct {
		ID               uuid.UUID
		StreamerUsername string
		UserID           uuid.UUID
		IsActive         bool
		EndTime          *time.Time
		Duration         int
	}
	now := time.Now()
	// Yeni sorgu: streamer_username ve user_id sütunlarını seçiyor.
	query := `SELECT id, streamer_username, user_id, is_active, end_time, duration FROM listeners WHERE is_active = true AND (end_time IS NULL OR end_time > $1);`
	rows, err := r.db.Query(query, now)
	if err != nil {
		return nil, fmt.Errorf("aktif listener'lar getirilirken hata: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var l struct {
			ID               uuid.UUID
			StreamerUsername string
			UserID           uuid.UUID
			IsActive         bool
			EndTime          *time.Time
			Duration         int
		}
		if err := rows.Scan(&l.ID, &l.StreamerUsername, &l.UserID, &l.IsActive, &l.EndTime, &l.Duration); err != nil {
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

func (r *Repository) GetUserRequestsForListener(listenerID uuid.UUID) ([]struct {
	UserID      uuid.UUID
	RequestTime time.Time
	EndTime     time.Time
}, error) {
	var requests []struct {
		UserID      uuid.UUID
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
			UserID      uuid.UUID
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

// GÜNCELLENDİ: InsertMessage fonksiyonu link bilgilerini de kaydediyor
func (r *Repository) InsertMessage(listenerID uuid.UUID, senderUsername, content string, timestamp time.Time, hasLink bool, extractedLinks []string) error {
	query := `INSERT INTO messages (listener_id, sender_username, content, message_timestamp, has_link, extracted_links) VALUES ($1, $2, $3, $4, $5, $6);`
	_, err := r.db.Exec(query, listenerID, senderUsername, content, timestamp, hasLink, extractedLinks)
	if err != nil {
		return fmt.Errorf("mesaj kaydedilirken hata: %w", err)
	}
	return nil
}

// YENİ: Listener durumunu güncellemek için
func (r *Repository) UpdateListenerStatus(listenerID uuid.UUID, isActive bool) error {
	query := `UPDATE listeners SET is_active = $1, updated_at = NOW() WHERE id = $2;`
	_, err := r.db.Exec(query, isActive, listenerID)
	if err != nil {
		return fmt.Errorf("listener durumu güncellenirken hata: %w", err)
	}
	return nil
}

// YENİ: Listener end_time güncellemek için
func (r *Repository) UpdateListenerEndTime(listenerID uuid.UUID, endTime time.Time) error {
	query := `UPDATE listeners SET end_time = $1, updated_at = NOW() WHERE id = $2;`
	_, err := r.db.Exec(query, endTime, listenerID)
	if err != nil {
		return fmt.Errorf("listener end_time güncellenirken hata: %w", err)
	}
	return nil
}

// YENİ: Mesajları çekmek için fonksiyonlar
func (r *Repository) GetMessagesByListener(listenerID uuid.UUID, limit, offset int) ([]struct {
	ID               uuid.UUID
	SenderUsername   string
	Content          string
	MessageTimestamp time.Time
	HasLink          bool
	ExtractedLinks   []string
}, error) {
	var messages []struct {
		ID               uuid.UUID
		SenderUsername   string
		Content          string
		MessageTimestamp time.Time
		HasLink          bool
		ExtractedLinks   []string
	}

	query := `SELECT id, sender_username, content, message_timestamp, has_link, extracted_links 
			  FROM messages 
			  WHERE listener_id = $1 
			  ORDER BY message_timestamp DESC 
			  LIMIT $2 OFFSET $3;`

	rows, err := r.db.Query(query, listenerID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("mesajlar getirilirken hata: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var msg struct {
			ID               uuid.UUID
			SenderUsername   string
			Content          string
			MessageTimestamp time.Time
			HasLink          bool
			ExtractedLinks   []string
		}

		// PostgreSQL array'ini Go slice'ına çevirmek için pq.Array kullanın
		if err := rows.Scan(&msg.ID, &msg.SenderUsername, &msg.Content, &msg.MessageTimestamp, &msg.HasLink, &msg.ExtractedLinks); err != nil {
			log.Printf("Mesaj satırı okunurken hata: %v", err)
			continue
		}
		messages = append(messages, msg)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("mesaj satır döngüsü hatası: %w", err)
	}

	return messages, nil
}
