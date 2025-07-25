package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"kick-chat/domain"
	"log"
	"time"

	"github.com/google/uuid"
)

func (r *Repository) InsertListener(ctx context.Context, streamerUsername string, kickUserID *int, profilePic *string, userID uuid.UUID, newIsActive bool, newEndTime *time.Time, newDuration int) (uuid.UUID, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return uuid.Nil, fmt.Errorf("transaction error: %w", err)
	}
	defer tx.Rollback() // Rollback on error or before successful commit

	var streamerID uuid.UUID
	var listenerID uuid.UUID

	// 1. Check if streamer exists, create if not
	err = tx.QueryRowContext(ctx, "SELECT id FROM streamers WHERE username = $1", streamerUsername).Scan(&streamerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Streamer does not exist, create a new one with provided kickUserID and profilePic
			// Use sql.NullInt32 and sql.NullString for potentially nil values
			var sqlKickUserID sql.NullInt32
			if kickUserID != nil {
				sqlKickUserID = sql.NullInt32{Int32: int32(*kickUserID), Valid: true}
			}
			var sqlProfilePic sql.NullString
			if profilePic != nil {
				sqlProfilePic = sql.NullString{String: *profilePic, Valid: true}
			}

			createStreamerQuery := `INSERT INTO streamers (username, kick_user_id, profile_pic) VALUES ($1, $2, $3) RETURNING id;`
			err = tx.QueryRowContext(ctx, createStreamerQuery, streamerUsername, sqlKickUserID, sqlProfilePic).Scan(&streamerID)
			if err != nil {
				return uuid.Nil, fmt.Errorf("failed to create new streamer: %w", err)
			}
			log.Printf("New streamer '%s' created with ID: %s (Kick ID: %v, Profile Pic: %v)\n", streamerUsername, streamerID, kickUserID, profilePic)
		} else {
			return uuid.Nil, fmt.Errorf("failed to query streamer: %w", err)
		}
	}

	// 2. Check if a listener entry already exists for this user and streamer
	var existingListenerID uuid.UUID
	var existingIsActive bool
	var existingEndTime sql.NullTime // Use sql.NullTime for nullable TIMESTAMP WITH TIME ZONE
	var existingDuration int

	getListenerQuery := `
		SELECT id, is_active, end_time, duration
		FROM listeners
		WHERE streamer_id = $1 AND user_id = $2;`

	err = tx.QueryRowContext(ctx, getListenerQuery, streamerID, userID).Scan(
		&existingListenerID,
		&existingIsActive,
		&existingEndTime,
		&existingDuration,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No existing listener, insert a new one
			// Validate newDuration
			if newDuration < 0 { // Assuming duration should not be negative
				return uuid.Nil, errors.New("duration cannot be negative")
			}

			insertListenerQuery := `
				INSERT INTO listeners (streamer_id, user_id, is_active, end_time, duration)
				VALUES ($1, $2, $3, $4, $5)
				RETURNING id;`
			err = tx.QueryRowContext(ctx, insertListenerQuery, streamerID, userID, newIsActive, newEndTime, newDuration).Scan(&listenerID)
			if err != nil {
				return uuid.Nil, fmt.Errorf("failed to insert new listener: %w", err)
			}
			log.Printf("New listener created for user %s and streamer %s with ID: %s\n", userID, streamerUsername, listenerID)
			return listenerID, tx.Commit() // Commit immediately if new listener created
		}
		return uuid.Nil, fmt.Errorf("failed to query existing listener: %w", err)
	}

	// 3. Existing listener found, update it
	listenerID = existingListenerID

	// Determine new duration and end_time based on existing and new values
	updatedIsActive := newIsActive // Assume the new request dictates active status
	updatedDuration := existingDuration
	var updatedEndTime *time.Time

	// Use the larger duration
	if newDuration > existingDuration {
		updatedDuration = newDuration
	}

	// If newEndTime is provided and is later than existing or existing is null
	if newEndTime != nil {
		if !existingEndTime.Valid || newEndTime.After(existingEndTime.Time) {
			updatedEndTime = newEndTime
		} else {
			updatedEndTime = &existingEndTime.Time // Keep existing if it's later
		}
	} else if existingEndTime.Valid {
		updatedEndTime = &existingEndTime.Time // Keep existing if new is null
	}

	// Validate duration for active listener: it should not be in the past
	if updatedIsActive && updatedEndTime != nil && updatedEndTime.Before(time.Now()) {
		// If the new request is to make it active, but the end time is in the past,
		// set updatedIsActive to false.
		updatedIsActive = false
		log.Printf("Warning: Listener for user %s, streamer %s tried to set active with past end_time (%v). Setting to inactive.\n", userID, streamerUsername, updatedEndTime)
	}

	updateListenerQuery := `
		UPDATE listeners
		SET is_active = $1, end_time = $2, duration = $3, updated_at = NOW()
		WHERE id = $4;`

	_, err = tx.ExecContext(ctx, updateListenerQuery, updatedIsActive, updatedEndTime, updatedDuration, listenerID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to update listener: %w", err)
	}

	log.Printf("Listener %s updated for user %s and streamer %s.\n", listenerID, userID, streamerUsername)

	return listenerID, tx.Commit()
}

func (r *Repository) InsertUserListenerRequest(listenerID uuid.UUID, userID uuid.UUID, requestTime time.Time, endTime time.Time) error {
	query := `INSERT INTO user_listener_requests (listener_id, user_id, request_time, end_time) VALUES ($1, $2, $3, $4);`
	_, err := r.db.Exec(query, listenerID, userID, requestTime, endTime)
	if err != nil {
		return fmt.Errorf("user listener request eklenirken hata: %w", err)
	}
	return nil
}

func (r *Repository) GetStreamerByUsername(ctx context.Context, username string) (*struct {
	ID         uuid.UUID
	KickUserID sql.NullInt32
	ProfilePic sql.NullString
}, error) {
	var streamerData struct {
		ID         uuid.UUID
		KickUserID sql.NullInt32
		ProfilePic sql.NullString
	}
	query := `SELECT id, kick_user_id, profile_pic FROM streamers WHERE username = $1;`
	err := r.db.QueryRowContext(ctx, query, username).Scan(&streamerData.ID, &streamerData.KickUserID, &streamerData.ProfilePic)
	if err == sql.ErrNoRows {
		return nil, nil // Streamer not found
	} else if err != nil {
		return nil, fmt.Errorf("failed to get streamer by username: %w", err)
	}
	return &streamerData, nil
}

func (r *Repository) GetListenerByStreamerIDAndUserID(ctx context.Context, streamerID, userID uuid.UUID) (*struct {
	ID         uuid.UUID
	StreamerID uuid.UUID // Now streamer_id
	UserID     uuid.UUID
	IsActive   bool
	EndTime    *time.Time
	Duration   int
}, error) {
	var listenerData struct {
		ID         uuid.UUID
		StreamerID uuid.UUID
		UserID     uuid.UUID
		IsActive   bool
		EndTime    *time.Time
		Duration   int
	}
	// Note: We no longer select streamer_username from listeners
	query := `SELECT id, streamer_id, user_id, is_active, end_time, duration FROM listeners WHERE streamer_id = $1 AND user_id = $2;`
	row := r.db.QueryRowContext(ctx, query, streamerID, userID)
	err := row.Scan(&listenerData.ID, &listenerData.StreamerID, &listenerData.UserID, &listenerData.IsActive, &listenerData.EndTime, &listenerData.Duration)
	if err == sql.ErrNoRows {
		return nil, nil // Listener not found for this specific user/streamer pair
	} else if err != nil {
		return nil, fmt.Errorf("failed to get listener by streamer ID and user ID: %w", err)
	}
	return &listenerData, nil
}
func (r *Repository) GetActiveListeners() ([]domain.ActiveListenerData, error) {
	var listeners []domain.ActiveListenerData
	now := time.Now()

	// Updated query: JOIN with 'streamers' table to get 'streamer.username'
	query := `
		SELECT
			l.id,
			l.streamer_id,
			s.username AS streamer_username,
			l.user_id,
			l.is_active,
			l.end_time,
			l.duration
		FROM
			listeners l
		JOIN
			streamers s ON l.streamer_id = s.id
		WHERE
			l.is_active = true AND (l.end_time IS NULL OR l.end_time > $1);` // Ensure end_time is in the future or NULL

	rows, err := r.db.Query(query, now)
	if err != nil {
		return nil, fmt.Errorf("aktif listener'lar getirilirken hata: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var l domain.ActiveListenerData // Use the defined struct
		if err := rows.Scan(
			&l.ID,
			&l.StreamerID, // Scan the new streamer_id
			&l.StreamerUsername,
			&l.UserID,
			&l.IsActive,
			&l.EndTime,
			&l.Duration,
		); err != nil {
			log.Printf("Satır okunurken hata: %v", err)
			continue // Continue to next row even if one fails
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

func (r *Repository) UpdateListenerEndTime(ctx context.Context, listenerID uuid.UUID, endTime time.Time) error {
	query := `UPDATE listeners SET end_time = $1, updated_at = NOW() WHERE id = $2;`
	_, err := r.db.ExecContext(ctx, query, endTime, listenerID)
	if err != nil {
		return fmt.Errorf("failed to update listener end time: %w", err)
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
