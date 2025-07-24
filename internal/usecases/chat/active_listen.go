package usecase

import (
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
)

func (u *listenUseCase) StartActiveListenersOnStartup() error {
	log.Println("Uygulama başlatılıyor: Aktif ve süresi dolmamış dinleyiciler kontrol ediliyor...")
	activeListeners, err := u.repo.GetActiveListeners()
	if err != nil {
		return fmt.Errorf("aktif dinleyiciler alınırken hata: %w", err)
	}

	for _, listenerDBData := range activeListeners {
		// Yalnızca aktif ve süresi henüz dolmamış dinleyicileri yeniden başlat
		if listenerDBData.IsActive && listenerDBData.EndTime != nil && listenerDBData.EndTime.After(time.Now()) {
			log.Printf("Veritabanından aktif dinleyici bulundu: '%s' (ID: %s). Yeniden başlatılıyor...", listenerDBData.StreamerUsername, listenerDBData.ID.String())

			ListenerManager.Lock() // ListenerManager map'ine erişirken kilit kullan
			listenerInfo, exists := ListenerManager.listeners[listenerDBData.StreamerUsername]

			if !exists {
				// Yeni bir ListenerInfo oluştur
				listenerInfo = &ListenerInfo{
					Username:       listenerDBData.StreamerUsername,
					UserRequests:   make(map[uuid.UUID]UserRequestInfo), // **BURADA MAP'İ BAŞLAT**
					OverallEndTime: *listenerDBData.EndTime,
					IsGlobalActive: true, // Başlangıçta aktif olarak işaretle
					ListenerDBID:   listenerDBData.ID,
					DataChannel:    make(chan Data, 100), // **BURADA KANALI BAŞLAT**
				}
				ListenerManager.listeners[listenerDBData.StreamerUsername] = listenerInfo
			} else {
				// Var olan ListenerInfo'yu güncelle
				listenerInfo.IsGlobalActive = true
				listenerInfo.OverallEndTime = *listenerDBData.EndTime
				listenerInfo.ListenerDBID = listenerDBData.ID

				// **VAR OLAN LISTENERINFO'DA MAP VE KANAL KONTROLÜ**
				if listenerInfo.UserRequests == nil {
					listenerInfo.UserRequests = make(map[uuid.UUID]UserRequestInfo)
				}
				if listenerInfo.DataChannel == nil {
					listenerInfo.DataChannel = make(chan Data, 100)
				}
			}

			// Kullanıcı isteklerini veritabanından yükle (eğer varsa)
			// UserRequests map'i yukarıda her zaman başlatıldığı için burada güvenle kullanılabilir
			if userRequests, reqErr := u.repo.GetUserRequestsForListener(listenerDBData.ID); reqErr == nil {
				for _, req := range userRequests {
					listenerInfo.UserRequests[req.UserID] = UserRequestInfo{
						UserID:      req.UserID,
						RequestTime: req.RequestTime,
						EndTime:     req.EndTime,
					}
				}
			} else {
				log.Printf("Dinleyici '%s' için kullanıcı istekleri yüklenirken hata: %v", listenerDBData.StreamerUsername, reqErr)
			}

			ListenerManager.Unlock() // Kilit burada serbest bırakılmalı

			// Dinlemeyi başlat
			// listenerInfo artık nil pointer olmamalı ve DataChannel/UserRequests başlatılmış olmalı
			go u.startListening(listenerInfo)

		} else if listenerDBData.IsActive {
			// Aktif ama süresi dolmuş veya EndTime nil olanları pasif yap
			log.Printf("Veritabanında süresi dolmuş veya geçersiz aktif dinleyici bulundu: '%s' (ID: %s). Pasif yapılıyor...", listenerDBData.StreamerUsername, listenerDBData.ID.String())
			if err := u.repo.UpdateListenerStatus(listenerDBData.ID, false); err != nil {
				log.Printf("Dinleyici '%s' (ID: %s) pasif yapılırken hata: %v", listenerDBData.StreamerUsername, listenerDBData.ID.String(), err)
			}
		}
	}
	log.Println("Aktif dinleyicileri başlatma işlemi tamamlandı.")
	return nil
}
