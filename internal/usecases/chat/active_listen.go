package usecase

import (
	"context"
	"fmt"
	"kick-chat/domain"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

func (u *listenUseCase) StartActiveListenersOnStartupa() error {
	log.Println("Uygulama başlatılıyor: Aktif ve süresi dolmamış dinleyiciler kontrol ediliyor...")
	activeListeners, err := u.repo.GetActiveListeners()
	if err != nil {
		return fmt.Errorf("aktif dinleyiciler alınırken hata: %w", err)
	}

	for _, listenerDBData := range activeListeners {
		// Yalnızca aktif ve süresi henüz dolmamış dinleyicileri yeniden başlat
		if listenerDBData.IsActive && listenerDBData.EndTime != nil && listenerDBData.EndTime.After(time.Now()) {
			log.Printf("Veritabanından aktif dinleyici bulundu: '%s' (ID: %s). Yeniden başlatılıyor...", listenerDBData.StreamerUsername, listenerDBData.ID.String())

			// ListenerManager.Lock() // ListenerManager map'ine erişirken kilit kullan
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

			// ListenerManager.Unlock() // Kilit burada serbest bırakılmalı

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

// Helper function to process individual listener during startup
func (u *listenUseCase) processStartupListener(listenerDBData domain.ActiveListenerData) error {
	// Check if listener is active and not expired
	if !listenerDBData.IsActive || listenerDBData.EndTime == nil || listenerDBData.EndTime.Before(time.Now()) {
		if listenerDBData.IsActive {
			// Active but expired - mark as inactive
			log.Printf("Süresi dolmuş aktif dinleyici bulundu: '%s' (ID: %s). Pasif yapılıyor...",
				listenerDBData.StreamerUsername, listenerDBData.ID.String())

			if err := u.repo.UpdateListenerStatus(listenerDBData.ID, false); err != nil {
				return fmt.Errorf("dinleyici pasif yapılırken hata: %w", err)
			}
		}
		return nil
	}

	log.Printf("Veritabanından aktif dinleyici bulundu: '%s' (ID: %s). Yeniden başlatılıyor...",
		listenerDBData.StreamerUsername, listenerDBData.ID.String())

	// Check if listener already exists in memory
	existingListener, exists := ListenerManager.GetListener(listenerDBData.StreamerUsername)

	var listenerInfo *ListenerInfo

	if !exists {
		// Create new ListenerInfo
		listenerInfo = &ListenerInfo{
			Username:       listenerDBData.StreamerUsername,
			UserRequests:   make(map[uuid.UUID]UserRequestInfo),
			OverallEndTime: *listenerDBData.EndTime,
			ListenerDBID:   listenerDBData.ID,
			DataChannel:    make(chan Data, u.config.MessageBufferSize),
			StopChannel:    make(chan struct{}),
			LastActivity:   time.Now(),
		}

		// Load user requests from database
		if err := u.loadUserRequestsForListener(listenerInfo); err != nil {
			log.Printf("Dinleyici '%s' için kullanıcı istekleri yüklenirken hata: %v",
				listenerDBData.StreamerUsername, err)
			// Continue anyway, don't fail the entire startup
		}

		ListenerManager.AddListener(listenerDBData.StreamerUsername, listenerInfo)
	} else {
		// Update existing ListenerInfo
		listenerInfo = existingListener
		listenerInfo.SetActive(true)

		// Update end time if needed
		listenerInfo.mu.Lock()
		if listenerDBData.EndTime.After(listenerInfo.OverallEndTime) {
			listenerInfo.OverallEndTime = *listenerDBData.EndTime
		}
		listenerInfo.ListenerDBID = listenerDBData.ID

		// Ensure channels are initialized
		if listenerInfo.DataChannel == nil {
			listenerInfo.DataChannel = make(chan Data, u.config.MessageBufferSize)
		}
		if listenerInfo.StopChannel == nil {
			listenerInfo.StopChannel = make(chan struct{})
		}
		if listenerInfo.UserRequests == nil {
			listenerInfo.UserRequests = make(map[uuid.UUID]UserRequestInfo)
		}
		listenerInfo.mu.Unlock()

		// Reload user requests
		if err := u.loadUserRequestsForListener(listenerInfo); err != nil {
			log.Printf("Dinleyici '%s' için kullanıcı istekleri yüklenirken hata: %v",
				listenerDBData.StreamerUsername, err)
		}
	}

	// Start listening in a new goroutine
	go u.startListening(listenerInfo)

	return nil
}
func (u *listenUseCase) StartActiveListenersOnStartup() error {
	log.Println("Uygulama başlatılıyor: Aktif ve süresi dolmamış dinleyiciler kontrol ediliyor...")

	activeListeners, err := u.repo.GetActiveListeners()
	if err != nil {
		return fmt.Errorf("aktif dinleyiciler alınırken hata: %w", err)
	}

	var wg sync.WaitGroup
	errorChan := make(chan error, len(activeListeners))

	for _, listenerDBData := range activeListeners {
		wg.Add(1)
		go func(data domain.ActiveListenerData) {
			defer wg.Done()

			if err := u.processStartupListener(data); err != nil {
				errorChan <- fmt.Errorf("listener '%s' başlatılırken hata: %w", data.StreamerUsername, err)
			}
		}(listenerDBData)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errorChan)

	// Collect any errors
	var errors []error
	for err := range errorChan {
		errors = append(errors, err)
		log.Printf("Startup error: %v", err)
	}

	// Start cleanup routine
	go ListenerManager.StartCleanupRoutine(context.Background())

	log.Println("Aktif dinleyicileri başlatma işlemi tamamlandı.")

	// Return first error if any
	if len(errors) > 0 {
		log.Printf("Toplam %d hata ile karşılaşıldı, devam ediliyor...", len(errors))
	}

	return nil
}

// Helper function to load user requests for a listener
func (u *listenUseCase) loadUserRequestsForListener(listenerInfo *ListenerInfo) error {
	userRequests, err := u.repo.GetUserRequestsForListener(listenerInfo.ListenerDBID)
	if err != nil {
		return fmt.Errorf("kullanıcı istekleri alınamadı: %w", err)
	}

	listenerInfo.mu.Lock()
	defer listenerInfo.mu.Unlock()

	now := time.Now()
	for _, req := range userRequests {
		// Only load non-expired requests
		if req.EndTime.After(now) {
			listenerInfo.UserRequests[req.UserID] = UserRequestInfo{
				UserID:      req.UserID,
				RequestTime: req.RequestTime,
				EndTime:     req.EndTime,
			}

			// Update overall end time if necessary
			if req.EndTime.After(listenerInfo.OverallEndTime) {
				listenerInfo.OverallEndTime = req.EndTime
			}
		}
	}

	log.Printf("Dinleyici '%s' için %d aktif kullanıcı isteği yüklendi",
		listenerInfo.Username, len(listenerInfo.UserRequests))

	return nil
}
