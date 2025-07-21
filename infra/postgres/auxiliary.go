package postgres

import (
	"errors"
	"log"
	"time"

	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

func (r *Repository) hashPassword(password string) (string, error) {

	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hashed), err
}
func (r *Repository) isDuplicateKeyError(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		// PostgreSQL error code for unique_violation
		return pqErr.Code == "23505"
	}

	return false
}

func (r *Repository) startCleanupJob(interval time.Duration) {
	ticker := time.NewTicker(interval)
	for range ticker.C {
		if err := r.cleanupExpiredActivations(); err != nil {
			log.Printf("cleanup error: %v", err)
		}
	}
}

func (r *Repository) cleanupExpiredActivations() error {
	const query = `DELETE FROM users WHERE is_active = false AND activation_expiry < NOW()`
	_, err := r.db.Exec(query)
	return err
}
