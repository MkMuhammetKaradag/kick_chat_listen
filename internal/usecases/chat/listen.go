package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"kick-chat/config"
	"log"
	"net/http"
	"regexp"
	"time"

	"github.com/google/uuid"
)

type ListenUseCase interface {
	Execute(ctx context.Context, username string) (string, error)
}
type ListenPostgresRepository interface {
	InsertListener(username string, isActive bool, endTime *time.Time, duration int) (uuid.UUID, error)
	InsertUserListenerRequest(listenerID uuid.UUID, userID string, requestTime time.Time, endTime time.Time) error
	GetListenerByUsername(username string) (*struct { // Anonim struct kullanabiliriz veya Listener struct'ını import edebiliriz.
		ID       uuid.UUID
		Username string
		IsActive bool
		EndTime  *time.Time
		Duration int
	}, error)
	GetActiveListeners() ([]struct { // Anonim struct
		ID       uuid.UUID
		Username string
		IsActive bool
		EndTime  *time.Time
		Duration int
	}, error)
	GetUserRequestsForListener(listenerID uuid.UUID) ([]struct {
		UserID      string
		RequestTime time.Time
		EndTime     time.Time
	}, error)
	InsertMessage(listenerID uuid.UUID, senderUsername, content string, timestamp time.Time) error
}

type listenUseCase struct {
	repo ListenPostgresRepository
}

// models paketindeki struct'ları buraya veya ilgili pakete taşıyın
type Message struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"` // JSON objesi olarak gelecek
}

type Sender struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
}
type Data struct {
	Type      string    `json:"type"`
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Sender    Sender    `json:"sender"`
	Timestamp time.Time `json:"timestamp"`
}

func NewListenUseCase(repo ListenPostgresRepository) ListenUseCase {
	return &listenUseCase{
		repo: repo,
	}
}

var linkRegex = regexp.MustCompile(`https?://[^\s]+`)

func (u *listenUseCase) Execute(ctx context.Context, username string) (string, error) {
	// TODO: Gerçek kullanıcı kimliğini ve dinleme süresini request body'den al
	// Şimdilik dummy veriler kullanıyoruz:
	currentUserID := "test_user_" + username // Gerçek kullanıcı ID'si ile değiştirilmeli
	durationToAdd := 7 * 24 * time.Hour      // Örnek: 7 gün
	endTimeToAdd := time.Now().Add(durationToAdd)
	fmt.Println("1")
	// ListenerManager'da ilgili listener'ı kontrol et

	if ListenerManager == nil {
		// Bu durum olmamalı, ListenerManager her zaman başlatılmış olmalı.
		log.Println("FATAL ERROR: ListenerManager başlatılmamış (nil)!")
		return "Sunucu hatası", fmt.Errorf("listener manager başlatılmamış")
	}
	ListenerManager.Lock()
	listenerInfo, exists := ListenerManager.listeners[username]
	ListenerManager.Unlock()
	fmt.Println("2")

	var listenerID uuid.UUID // Veritabanındaki Listener ID'si

	if !exists || !listenerInfo.IsGlobalActive {
		// --- Yeni Listener Oluşturma Süreci ---
		log.Printf("'%s' için yeni listener süreci başlatılıyor...", username)

		// 1. Veritabanında Listener kaydını kontrol et/oluştur
		dbListenerData, err := u.repo.GetListenerByUsername(username)
		if err != nil {
			log.Printf("'%s' için veritabanı kontrol hatası: %v", username, err)
			return "veritabanı hatası", err
		}

		if dbListenerData == nil {
			// Listener yok, veritabanına yeni Listener kaydı ekle
			log.Printf("'%s' için veritabanında Listener kaydı bulunamadı, oluşturuluyor...", username)
			newListenerID, err := u.repo.InsertListener(username, true, &endTimeToAdd, int(durationToAdd.Seconds()))
			if err != nil {
				log.Printf("Veritabanına yeni listener eklenirken hata: %v", err)
				return "Veritabanı hatası.", err
			}
			listenerID = newListenerID
			log.Printf("Yeni listener veritabanına eklendi: ID=%s", listenerID)

			// ListenerInfo nesnesini oluştur ve ListenerManager'a ekle
			listenerInfo = &ListenerInfo{
				Username:       username,
				UserRequests:   make(map[string]UserRequestInfo),
				OverallEndTime: endTimeToAdd,
				IsGlobalActive: true,
				ListenerDBID:   listenerID,
				// DataChannel henüz atanmadı, startListening'de atanacak
			}
			ListenerManager.Lock()
			ListenerManager.listeners[username] = listenerInfo
			ListenerManager.Unlock()

		} else {
			// Listener veritabanında var ama ListenerManager'da yok (örn: sunucu restart)
			log.Printf("Listener veritabanında mevcut, ListenerManager'a yeniden ekleniyor: Username=%s, ID=%s", dbListenerData.Username, dbListenerData.ID)
			listenerID = dbListenerData.ID

			// ListenerManager'da yoksa oluştur, varsa süresini güncelle (en geç olana göre)
			ListenerManager.Lock()
			if existingListenerInfo, ok := ListenerManager.listeners[username]; ok {
				// Zaten ListenerManager'da varsa, sadece süreyi güncellemeliyiz
				if endTimeToAdd.After(existingListenerInfo.OverallEndTime) {
					existingListenerInfo.OverallEndTime = endTimeToAdd
					// ListenerManager.listeners[username].OverallEndTime = endTimeToAdd // Bu doğrudan erişim yerine SetOverallEndTime gibi bir metod olabilir
					// Veritabanı güncellemesi de yapılmalı
					// if dbErr := u.repo.UpdateListenerEndTime(listenerID, endTimeToAdd); dbErr != nil {
					log.Printf("'%s' için ListenerManager'da süreyi güncellerken veritabanı hatası:", username)
					// }
				}
				listenerInfo = existingListenerInfo // Mevcut ListenerInfo'yu kullan
			} else {
				// ListenerManager'da yoksa yenisini oluştur
				listenerInfo = &ListenerInfo{
					Username:       username,
					UserRequests:   make(map[string]UserRequestInfo),
					OverallEndTime: *dbListenerData.EndTime, // Veritabanındaki mevcut end_time
					IsGlobalActive: dbListenerData.IsActive,
					ListenerDBID:   listenerID,
				}
				// Veritabanından mevcut user request'leri çekip listenerInfo.UserRequests'ı doldur
				if userRequests, reqErr := u.repo.GetUserRequestsForListener(listenerID); reqErr == nil {
					for _, req := range userRequests {
						listenerInfo.UserRequests[req.UserID] = UserRequestInfo{
							UserID:      req.UserID,
							RequestTime: req.RequestTime,
							EndTime:     req.EndTime,
						}
					}
				} else {
					log.Printf("'%s' için mevcut user request'leri yüklerken hata: %v", username, reqErr)
				}

				// Eğer ListenerManager'daki OverallEndTime ile yeni gelen endTimeToAdd karşılaştırıp en geç olanı al
				if endTimeToAdd.After(listenerInfo.OverallEndTime) {
					listenerInfo.OverallEndTime = endTimeToAdd
					// if dbErr := database.UpdateListenerEndTime(listenerID, endTimeToAdd); dbErr != nil {
					log.Printf("'%s' için ListenerManager'da süreyi güncellerken veritabanı hatası:", username)
					// }
				}

				ListenerManager.listeners[username] = listenerInfo // ListenerManager'a ekle
			}
			ListenerManager.Unlock()
		}

		// Veritabanına yeni UserListenerRequest ekle
		err = u.repo.InsertUserListenerRequest(listenerID, currentUserID, time.Now(), endTimeToAdd)
		if err != nil {
			log.Printf("Veritabanına yeni user listener request eklenirken hata: %v", err)
			// Hata durumunda ne yapılmalı? Şimdilik devam edelim.
		} else {
			// ListenerInfo'daki UserRequests map'ini güncelle
			listenerInfo.Lock()
			listenerInfo.UserRequests[currentUserID] = UserRequestInfo{
				UserID:      currentUserID,
				RequestTime: time.Now(),
				EndTime:     endTimeToAdd,
			}
			// Global süreyi de güncellemek gerekebilir (eğer yeni istek daha geç bitiyorsa)
			if endTimeToAdd.After(listenerInfo.OverallEndTime) {
				listenerInfo.OverallEndTime = endTimeToAdd
				// Veritabanındaki Listener kaydının EndTime'ını da güncellemeliyiz.
				// if dbErr := database.UpdateListenerEndTime(listenerID, endTimeToAdd); dbErr != nil {
				log.Printf("'%s' için listener end time güncellenirken veritabanı hatası: %v", username)
				// }
			}
			listenerInfo.Unlock()
			log.Printf("'%s' için yeni user request eklendi. (User: %s, Ends: %s)", username, currentUserID, endTimeToAdd.Format(time.RFC3339))
		}

		// Sohbet dinlemeyi başlat (eğer global olarak aktif değilse)
		if listenerInfo.IsGlobalActive {
			log.Printf("'%s' için sohbet dinleme başlatılıyor...", username)
			// DataChannel'ı burada oluşturup ListenerInfo'ya ata
			newDataChannel := make(chan Data, 100) // Bufferlı kanal
			listenerInfo.DataChannel = newDataChannel

			// startListening fonksiyonunu çağır
			go u.startListening(listenerInfo)

			// Veritabanındaki is_active durumunu true olarak güncelle
			// if dbErr := u.repo.UpdateListenerStatus(listenerID, true); dbErr != nil {
			// 	log.Printf("'%s' için listener aktif durumu güncellenirken hata: %v", username, dbErr)
			// } else {
			// 	listenerInfo.IsGlobalActive = true // ListenerInfo'yu da güncelle
			// }
		}

		// Kullanıcıya yanıt döndür
		return fmt.Sprintf("'%s' kullanıcısının sohbeti dinlenmeye başlandı.", username), nil

	} else {
		// --- Mevcut listener var ve aktif ---
		log.Printf("'%s' kullanıcısı zaten dinleniyor. Yeni kullanıcı isteği işleniyor.", username)

		// Veritabanında bu listener için yeni bir UserListenerRequest ekle
		err := u.repo.InsertUserListenerRequest(listenerInfo.ListenerDBID, currentUserID, time.Now(), endTimeToAdd)
		if err != nil {
			log.Printf("Mevcut listener için veritabanına yeni user listener request eklenirken hata: %v", err)
			return "Veritabanı hatası.", err
		}

		// ListenerInfo'daki UserRequests map'ini güncelle
		listenerInfo.Lock()
		listenerInfo.UserRequests[currentUserID] = UserRequestInfo{
			UserID:      currentUserID,
			RequestTime: time.Now(),
			EndTime:     endTimeToAdd,
		}
		// Global süreyi de güncellemek gerekebilir
		if endTimeToAdd.After(listenerInfo.OverallEndTime) {
			listenerInfo.OverallEndTime = endTimeToAdd
			// Veritabanındaki Listener kaydının EndTime'ını da güncellemeliyiz.
			// if dbErr := u.repo.UpdateListenerEndTime(listenerInfo.ListenerDBID, endTimeToAdd); dbErr != nil {
			log.Printf("'%s' için listener end time güncellenirken veritabanı hatası: %v", username)
			// }
		}
		listenerInfo.Unlock()
		log.Printf("'%s' için yeni user request eklendi. (User: %s, Ends: %s)", username, currentUserID, endTimeToAdd.Format(time.RFC3339))

		// Kullanıcıya yanıt döndür
		return fmt.Sprintf("'%s' kullanıcısının sohbetini dinleme süreniz %s olarak ayarlandı.", username, endTimeToAdd.Format(time.RFC3339)), nil
	}

}
func getKnownChatId(username string) int {
	// Örnek: Bilinen ID'leri bir map'te tut
	knownChatIds := map[string]int{
		// "buraksakinol": 25461130, // Sizin bildiğiniz ID'leri buraya ekleyebilirsiniz.
	}
	if chatId, exists := knownChatIds[username]; exists {
		return chatId
	}
	return 0
}

var GetChatIdFromKick = func(username string) (int, error) {
	methods := []func(string) (int, error){
		getChatIdFromRapidAPI,
	}
	for _, method := range methods {
		chatId, err := method(username)
		if err == nil {
			return chatId, nil
		}
	}
	return 0, fmt.Errorf("tüm yöntemler başarısız")

}

func getChatIdFromRapidAPI(username string) (int, error) {
	apikey := "test"
	fmt.Println("rapid_api:", config.GetRapidAPIKey())
	fmt.Println("rapid_api:", apikey)

	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("https://kick-com-api.p.rapidapi.com/channels/%s/chatroom", username)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("istek oluşturulamadı: %w", err)
	}
	req.Header.Add("x-rapidapi-key", apikey)
	req.Header.Add("x-rapidapi-host", "kick-com-api.p.rapidapi.com")
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("istek gönderilemedi: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("RapidAPI isteği başarısız: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("cevap okunamadı: %w", err)
	}
	var result struct {
		Data struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("JSON parse hatası: %w", err)
	}
	if result.Data.ID == 0 {
		return 0, fmt.Errorf("chat ID bulunamadı (0 döndü)")
	}
	return result.Data.ID, nil
}

// func askForManualChatId(username string) (int, error) {
// 	reader := bufio.NewReader(os.Stdin)
// 	fmt.Println(aurora.Yellow(fmt.Sprintf("🔧 %s için chat ID'sini biliyorsanız girebilirsiniz", username)))
// 	fmt.Println(aurora.White("Chat ID'sini nasıl bulacağınız:"))
// 	fmt.Println(aurora.White(fmt.Sprintf("1. Tarayıcınızda https://kick.com/%s adresine gidin", username)))
// 	fmt.Println(aurora.White("2. F12 tuşuna basın -> Network sekmesine gidin"))
// 	fmt.Println(aurora.White("3. Sayfayı yenileyin ve 'chatrooms' içeren istekleri arayın"))
// 	fmt.Println(aurora.White("4. İstek URL'sinde 'chatrooms.XXXXX.v2' formatında XXXXX sayısı chat ID'dir"))
// 	fmt.Println(aurora.White(""))
// 	fmt.Print(aurora.White("Chat ID'yi girin (boş bırakmak için Enter): "))
// 	idStr, _ := reader.ReadString('\n')
// 	idStr = strings.TrimSpace(idStr)
// 	if idStr == "" {
// 		return 0, nil // Boş bırakılırsa sıfır döndür, hata değil
// 	}
// 	chatId, err := strconv.Atoi(idStr)
// 	if err != nil {
// 		return 0, fmt.Errorf("geçersiz ID formatı: %s", idStr)
// 	}
// 	return chatId, nil
// }

// Mesajı parse edip ListenerInfo'nun DataChannel'ına gönderen yardımcı fonksiyon
func unmarshallAndSendToChannel(info *ListenerInfo, msgByte []byte) {
	var event Message
	if err := json.Unmarshal(msgByte, &event); err != nil {
		return // Yanlış formatta mesajları görmezden gel
	}

	if event.Event == "App\\Events\\ChatMessageEvent" {
		var rawDataString string
		if err := json.Unmarshal(event.Data, &rawDataString); err != nil {
			return
		}
		var data Data
		if err := json.Unmarshal([]byte(rawDataString), &data); err != nil {
			return
		}
		if data.Type == "message" {
			// Mesajı ListenerInfo'nun DataChannel'ına gönder
			// Kanal doluysa bloke olmamak için select kullanabiliriz.
			select {
			case info.DataChannel <- data:
				// Başarıyla gönderildi
			default:
				// Kanal dolu, mesajı atla veya logla
				log.Printf("'%s' için data channel dolu, mesaj atlanıyor.", info.Username)
			}
		}
	}
}
