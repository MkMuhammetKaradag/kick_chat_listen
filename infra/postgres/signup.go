package postgres

import (
	"context"
	"fmt"
	"kick-chat/domain"

	"github.com/google/uuid"
)

func (r *Repository) SignUp(ctx context.Context, user *domain.User) (uuid.UUID, error) {
	// 1. Şifre hashleme
	hashedPassword, err := r.hashPassword(user.Password)
	if err != nil {
		return uuid.Nil, fmt.Errorf("hashing error: %w", err)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return uuid.Nil, fmt.Errorf("transaction error: %w", err)
	}
	defer tx.Rollback()

	query := `
		INSERT INTO users (
			username, email, password,failed_login_attempts
		) VALUES ($1, $2, $3,$4) RETURNING id`

	var auserID uuid.UUID
	err = tx.QueryRowContext(ctx, query,
		user.Username,
		user.Email,
		hashedPassword,
		0,
	).Scan(&auserID)

	if err != nil {
		if r.isDuplicateKeyError(err) {
			return uuid.Nil, fmt.Errorf("username or email already exists: %w", err)
		}
		return uuid.Nil, fmt.Errorf("insert error: %w", err)
	}

	// 6. Commit
	if err := tx.Commit(); err != nil {
		return uuid.Nil, fmt.Errorf("commit error: %w", err)
	}

	return auserID, nil
}
