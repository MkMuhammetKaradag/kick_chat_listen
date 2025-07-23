package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"kick-chat/domain"

	"golang.org/x/crypto/bcrypt"
)

func (r *Repository) SignIn(ctx context.Context, identifier, password string) (*domain.User, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("transaction error: %w", err)
	}
	defer tx.Rollback()

	const query = `
		SELECT id, username, email, password,failed_login_attempts
		FROM users
		WHERE (username = $1 OR email = $1)`

	var auth domain.User
	var hashedPassword string
	var failedAttempts int

	err = tx.QueryRowContext(ctx, query, identifier).Scan(
		&auth.ID,
		&auth.Username,
		&auth.Email,
		&hashedPassword,
		&failedAttempts,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("query error: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password)); err != nil {
		// Başarısız giriş denemesini kaydet
		_, err := tx.ExecContext(ctx, "UPDATE users SET failed_login_attempts = $1 WHERE id = $2", failedAttempts+1, auth.ID)
		if err != nil {
			// Loglama mekanizması burada kullanılabilir
			fmt.Printf("Failed to update login attempts: %v\n", err)
		}
		return nil, ErrInvalidCredentials
	}

	// Başarılı giriş, deneme sayacını sıfırla ve son giriş zamanını güncelle
	_, err = tx.ExecContext(ctx, "UPDATE users SET failed_login_attempts = 0, last_login = NOW() WHERE id = $1", auth.ID)
	if err != nil {
		// Loglama mekanizması burada kullanılabilir
		fmt.Printf("Failed to update last login: %v\n", err)
	}

	return &auth, tx.Commit()
}
