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
    		is_active BOOLEAN DEFAULT false,
    		is_email_verified BOOLEAN DEFAULT false,  
			activation_code VARCHAR(6),          
			activation_id UUID  DEFAULT gen_random_uuid(),    
			activation_expiry TIMESTAMP WITH TIME ZONE,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			failed_login_attempts INT DEFAULT 0,
			account_locked BOOLEAN DEFAULT false,
			lock_until TIMESTAMP WITH TIME ZONE,
			last_login TIMESTAMP WITH TIME ZONE,      
			is_2fa_enabled BOOLEAN DEFAULT false,
			CONSTRAINT check_activation_code CHECK (
				activation_code ~ '^[0-9]{6}$'  
			)
		)`
	createForgotPasswordsTable = `
		CREATE TABLE IF NOT EXISTS forgot_passwords (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			token TEXT NOT NULL,
			attempt_count INT DEFAULT 0,
			expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		)`
	// Bizim proje için Listener tablosu
	createListenersTable = `
		CREATE TABLE IF NOT EXISTS listeners (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			username VARCHAR(50) NOT NULL UNIQUE, -- Dinlenen yayıncı adı
			is_active BOOLEAN DEFAULT true NOT NULL, -- Şu anda global olarak dinleniyor mu?
			end_time TIMESTAMP WITH TIME ZONE NULL, -- Belirli bir tarihe kadar dinlenecekse
			duration INT DEFAULT 0 NOT NULL, -- Saniye cinsinden toplam dinleme süresi
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);`

	// Bizim proje için UserListenerRequest tablosu
	createUserListenerRequestsTable = `
		CREATE TABLE IF NOT EXISTS user_listener_requests (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			listener_id UUID NOT NULL, -- Hangi Listener'a ait (Foreign Key)
			user_id VARCHAR(50) NOT NULL, -- Senin sistemindeki kullanıcının ID'si (username olabilir)
			request_time TIMESTAMP WITH TIME ZONE NOT NULL, -- Bu isteğin başladığı zaman
			end_time TIMESTAMP WITH TIME ZONE NOT NULL, -- Bu isteğin biteceği zaman
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			FOREIGN KEY (listener_id) REFERENCES listeners(id) ON DELETE CASCADE
		);`
)

func initDB(db *sql.DB) error {
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
