package postgres

import (
	"database/sql"
	"fmt"
	"log"
)

const (
	createUsersTable = `
		CREATE TABLE IF NOT EXISTS users (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			username VARCHAR(50) NOT NULL UNIQUE,
			email VARCHAR(100) NOT NULL UNIQUE,
			password TEXT NOT NULL,
			failed_login_attempts INT DEFAULT 0,
			last_login TIMESTAMP WITH TIME ZONE, 	
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		)`

	// Yeni streamers tablosu
	createStreamersTable = `
		CREATE TABLE IF NOT EXISTS streamers (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			username VARCHAR(50) NOT NULL UNIQUE, -- Yayıncının kullanıcı adı (StreamerUsername)
			kick_user_id INT UNIQUE, -- Kick platformundaki kullanıcı ID'si
			profile_pic TEXT, -- Profil fotoğrafı URL'si veya bilgisi
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);`

	// listener tablosu (streamer_id ile güncellendi)
	createListenersTable = `
		CREATE TABLE IF NOT EXISTS listeners (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			streamer_id UUID REFERENCES streamers(id) ON DELETE CASCADE, -- Yeni: Streamers tablosuna referans
			user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			is_active BOOLEAN DEFAULT true NOT NULL,
			end_time TIMESTAMP WITH TIME ZONE NULL,
			duration INT DEFAULT 0 NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			UNIQUE(streamer_id, user_id) -- Bir kullanıcının aynı yayıncıyı birden fazla dinlemesini engeller
		);`

	// UserListenerRequest tablosu
	createUserListenerRequestsTable = `
		CREATE TABLE IF NOT EXISTS user_listener_requests (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			listener_id UUID NOT NULL,
			user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			request_time TIMESTAMP WITH TIME ZONE NOT NULL,
			end_time TIMESTAMP WITH TIME ZONE NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			FOREIGN KEY (listener_id) REFERENCES listeners(id) ON DELETE CASCADE
		);`
)

func initDB(db *sql.DB) error {
	// Streamers tablosunu ilk oluştur
	if _, err := db.Exec(createStreamersTable); err != nil {
		return fmt.Errorf("failed to create streamers table: %w", err)
	}

	if _, err := db.Exec(createUsersTable); err != nil {
		return fmt.Errorf("failed to create users table: %w", err)
	}
	if _, err := db.Exec(createListenersTable); err != nil {
		return fmt.Errorf("listeners tablosu oluşturulamadı: %w", err)
	}
	if _, err := db.Exec(createUserListenerRequestsTable); err != nil {
		return fmt.Errorf("user_listener_requests tablosu oluşturulamadı: %w", err)
	}

	log.Println("Database tables initialized")
	return nil
}
