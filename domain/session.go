package domain

import "time"

// Session, kullanıcı oturum bilgilerini tutan yapı
type Session struct {
	UserID string    `json:"id"`     // Kullanıcı ID'si
	Device string    `json:"device"` // Kullanıcı adı
	Ip     string    `json:"ip"`     // E-posta adresi
	Expiry time.Time `json:"expiry"` // Oturum sona erme zamanı
}
