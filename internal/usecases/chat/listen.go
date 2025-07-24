package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"kick-chat/domain"
	"kick-chat/internal/middleware"
	"kick-chat/utils"
	"log"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/logrusorgru/aurora"

	"github.com/gorilla/websocket"
)

type ListenUseCase interface {
	Execute(fbrCtx *fiber.Ctx, ctx context.Context, username string) (string, error)
}

type ListenPostgresRepository interface {
	// Değiştirildi
	InsertListener(streamerUsername string, userID uuid.UUID, isActive bool, endTime *time.Time, duration int) (uuid.UUID, error)
	// user_id'nin UUID olduğundan emin ol
	InsertUserListenerRequest(listenerID uuid.UUID, userID uuid.UUID, requestTime time.Time, endTime time.Time) error
	// Değiştirildi
	GetListenerByStreamerUsername(streamerUsername string) (*struct {
		ID               uuid.UUID
		StreamerUsername string
		UserID           uuid.UUID
		IsActive         bool
		EndTime          *time.Time
		Duration         int
	}, error)
	// Değiştirildi
	GetActiveListeners() ([]struct {
		ID               uuid.UUID
		StreamerUsername string
		UserID           uuid.UUID
		IsActive         bool
		EndTime          *time.Time
		Duration         int
	}, error)
	// Değiştirildi
	GetUserRequestsForListener(listenerID uuid.UUID) ([]struct {
		UserID      uuid.UUID
		RequestTime time.Time
		EndTime     time.Time
	}, error)
	// GÜNCELLENDİ: InsertMessage fonksiyonu link bilgilerini de alıyor
	InsertMessage(listenerID uuid.UUID, senderUsername, content string, timestamp time.Time, hasLink bool, extractedLinks []string) error
	// YENİ: Eklenen fonksiyonlar
	UpdateListenerStatus(listenerID uuid.UUID, isActive bool) error
	UpdateListenerEndTime(listenerID uuid.UUID, endTime time.Time) error
	GetMessagesByListener(listenerID uuid.UUID, limit, offset int) ([]struct {
		ID               uuid.UUID
		SenderUsername   string
		Content          string
		MessageTimestamp time.Time
		HasLink          bool
		ExtractedLinks   []string
	}, error)
}

type listenUseCase struct {
	repo ListenPostgresRepository
}

// Models - mevcut struct'larınız aynı kalıyor
type Message struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}
type Identity struct {
	Color string `json:"color"`
}

type Sender struct {
	ID       int      `json:"id"`
	Username string   `json:"username"`
	Identity Identity `json:"identity"`
}

type Data struct {
	Type      string    `json:"type"`
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Sender    Sender    `json:"sender"`
	Timestamp time.Time `json:"timestamp"`
}

// --- Struct Tanımları ---
// Config struct'ı (eğer usecase içinde kullanılıyorsa)
type Config struct {
	WebSocketUrl             string
	ChatroomSubscribeCommand string
	BatchSize                int
}

type UserRequestInfo struct {
	UserID      uuid.UUID
	RequestTime time.Time
	EndTime     time.Time
}

type ListenerInfo struct {
	Username       string
	Client         *websocket.Conn
	UserRequests   map[uuid.UUID]UserRequestInfo
	OverallEndTime time.Time
	IsGlobalActive bool
	ListenerDBID   uuid.UUID
	DataChannel    chan<- Data // Mesajları işlemek için

	sync.Mutex // Veya sync.RWMutex kullanabilirsiniz

}

// ListenerManager struct'ı
type ListenerManagerType struct {
	sync.RWMutex
	listeners map[string]*ListenerInfo
}

// Global ListenerManager örneği
var ListenerManager *ListenerManagerType
var AppConfig *Config = &Config{
	BatchSize:                10,
	ChatroomSubscribeCommand: "{\"event\":\"pusher:subscribe\",\"data\":{\"auth\":\"\",\"channel\":\"chatrooms.%d.v2\"}}",
	WebSocketUrl:             "wss://ws-us2.pusher.com/app/32cbd69e4b950bf97679?protocol=7&client=js&version=8.4.0&flash=false",
} // Eğer config'i de buraya alırsanız

// --- Helper Fonksiyonlar ---
// getKnownChatId, askForManualChatId, GetChatIdFromKick, startListening, unmarshallAndSendToChannel, InsertMessage vb. fonksiyonlar
// getChatId fonksiyonu: Hem bilinen ID'leri, hem API'yi, hem de manuel girişi birleştirmeli.
func getChatId(username string) (int, error) {
	// 1. Bilinen ID'leri kontrol et
	if knownId := getKnownChatId(username); knownId != 0 {
		fmt.Println("localde kayıtlıydı:", knownId)
		return knownId, nil
	}

	// 2. API'den almaya çalış (RapidAPI)
	chatId, err := GetChatIdFromKick(username)
	if err != nil {
		return 0, err
	}

	return chatId, nil

}
func (u *listenUseCase) startListening(info *ListenerInfo) {
	info.Lock()
	info.IsGlobalActive = true
	info.Unlock()
	defer func() {
		log.Printf("'%s' için sohbet dinleme goroutine'i sonlanıyor.", info.Username)
		// Goroutine sona erdiğinde IsGlobalActive'ı false yap.
		info.Lock()
		info.IsGlobalActive = false
		info.Unlock()

		// Veritabanında dinleyicinin durumunu pasif olarak güncelle.
		if err := u.repo.UpdateListenerStatus(info.ListenerDBID, false); err != nil {
			log.Printf("'%s' için veritabanı listener durumu güncellenirken hata: %v", info.Username, err)
		}

		// ListenerManager'dan bu dinleyiciyi kaldır.
		// Bu, bir sonraki isteğin yeni bir dinleyici başlatmasına izin verir.
		ListenerManager.Lock()
		delete(ListenerManager.listeners, info.Username)
		ListenerManager.Unlock()

		if info.Client != nil {
			info.Client.Close() // WebSocket bağlantısını kapat
		}
	}()

	// ... (Chat ID alma mantığı) ...
	chatId, err := getChatId(info.Username)
	if err != nil {
		log.Printf("'%s' için chat ID alınamadı: %v. Dinleme başlatılamıyor.", info.Username, err)
		return // Hata durumunda defer çalışacak
	}
	log.Printf("'%s' için Chat ID: %d bulundu.", info.Username, chatId)

	// WebSocket bağlantısı kurma
	dialer := websocket.Dialer{}
	wsURL := AppConfig.WebSocketUrl
	subscribeMsg := fmt.Sprintf(AppConfig.ChatroomSubscribeCommand, chatId)

	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		log.Printf("❌ '%s' (ID: %d) için websocket bağlantı hatası: %v", info.Username, chatId, err)
		return // Hata durumunda defer çalışacak
	}
	info.Client = conn
	log.Printf("✅ '%s' (ID: %d) için websocket bağlandı.", info.Username, chatId)

	err = conn.WriteMessage(websocket.TextMessage, []byte(subscribeMsg))
	if err != nil {
		log.Printf("❌ '%s' (ID: %d) için subscribe mesajı gönderme hatası: %v", info.Username, chatId, err)
		return // Hata durumunda defer çalışacak
	}
	log.Printf("✅ '%s' (ID: %d) için subscribe mesajı gönderildi.", info.Username, chatId)

	// Mesaj dinleme döngüsünü başlat
	for {
		info.Lock()
		isActive := info.IsGlobalActive
		overallEndTime := info.OverallEndTime
		numUserRequests := len(info.UserRequests)
		info.Unlock()

		// Dinleme koşulları artık geçerli değilse döngüden çık.
		// Bu, IsGlobalActive false olduğunda veya tüm kullanıcı isteklerinin süresi dolduğunda gerçekleşir.
		if !isActive || (overallEndTime.Before(time.Now()) && numUserRequests == 0) {
			log.Printf("'%s' için dinleme koşulları artık geçerli değil. Kapatılıyor.", info.Username)
			break // Döngüden çık, defer temizliği yapacak
		}

		_, msgByte, readErr := conn.ReadMessage()
		if readErr != nil {
			log.Printf("❌ '%s' için mesaj okuma hatası: %v", info.Username, readErr)
			// Yeniden bağlanma mekanizması eklenebilir, şimdilik döngüden çıkıyoruz.
			break
		}

		go u.unmarshallAndSendToChannel(info, msgByte)
	}
}

// NewListenerManager constructor
func NewListenerManager() *ListenerManagerType {
	return &ListenerManagerType{
		listeners: make(map[string]*ListenerInfo),
	}
}

func NewListenUseCase(repo ListenPostgresRepository) ListenUseCase {
	return &listenUseCase{
		repo: repo,
	}
}

var linkRegex = regexp.MustCompile(`https?://[^\s]+`)

func (u *listenUseCase) Execute(fbrCtx *fiber.Ctx, ctx context.Context, username string) (string, error) {
	userData, ok := middleware.GetUserData(fbrCtx)
	if !ok {
		return "", domain.ErrNotFoundAuthorization

	}
	currentUserID, err := uuid.Parse(userData.UserID)
	if err != nil {
		return "", domain.ErrNotFoundAuthorization

	}
	durationToAdd := 5 * time.Hour
	endTimeToAdd := time.Now().Add(durationToAdd)

	if ListenerManager == nil {
		log.Println("FATAL ERROR: ListenerManager başlatılmamış (nil)!")
		return "Sunucu hatası", fmt.Errorf("listener manager başlatılmamış")
	}

	ListenerManager.Lock()
	defer ListenerManager.Unlock()
	listenerInfo, exists := ListenerManager.listeners[username]
	// ListenerManager.Unlock()

	var listenerID uuid.UUID
	shouldStartNewListener := false
	if !exists {
		// Bellekte bu yayıncı için bir dinleyici yok, veritabanını kontrol et.
		dbListenerData, err := u.repo.GetListenerByStreamerUsername(username)
		if err != nil {
			log.Printf("'%s' için veritabanı kontrol hatası: %v", username, err)
			return "veritabanı hatası", err
		}

		if dbListenerData == nil {
			// Veritabanında da yok, yeni bir dinleyici kaydı oluştur.
			log.Printf("'%s' için veritabanında Listener kaydı bulunamadı, oluşturuluyor...", username)
			newListenerID, err := u.repo.InsertListener(username, currentUserID, true, &endTimeToAdd, int(durationToAdd.Seconds()))
			if err != nil {
				log.Printf("Veritabanına yeni listener eklenirken hata: %v", err)
				return "Veritabanı hatası.", err
			}
			listenerID = newListenerID
			listenerInfo = &ListenerInfo{
				Username:       username,
				UserRequests:   make(map[uuid.UUID]UserRequestInfo),
				OverallEndTime: endTimeToAdd,
				IsGlobalActive: false, // startListening başlatıldığında true olacak
				ListenerDBID:   listenerID,
				DataChannel:    make(chan Data, 100), // Kanalı burada oluştur
			}
			ListenerManager.listeners[username] = listenerInfo
			shouldStartNewListener = true // Yeni dinleyici başlatılmalı
		} else {
			// Veritabanında var ama bellekte yok, belleğe geri yükle.
			listenerID = dbListenerData.ID
			listenerInfo = &ListenerInfo{
				Username:       username,
				UserRequests:   make(map[uuid.UUID]UserRequestInfo),
				OverallEndTime: *dbListenerData.EndTime,
				IsGlobalActive: dbListenerData.IsActive, // Veritabanındaki aktiflik durumunu kullan
				ListenerDBID:   listenerID,
				DataChannel:    make(chan Data, 100), // Kanalı burada oluştur
			}

			if userRequests, reqErr := u.repo.GetUserRequestsForListener(listenerID); reqErr == nil {
				for _, req := range userRequests {
					listenerInfo.UserRequests[req.UserID] = UserRequestInfo{
						UserID:      req.UserID,
						RequestTime: req.RequestTime,
						EndTime:     req.EndTime,
					}
				}
			}

			// Yeni isteğin süresi, genel bitiş süresini uzatıyorsa güncelle.
			if endTimeToAdd.After(listenerInfo.OverallEndTime) {
				listenerInfo.OverallEndTime = endTimeToAdd
				if dbErr := u.repo.UpdateListenerEndTime(listenerID, endTimeToAdd); dbErr != nil {
					log.Printf("'%s' için listener end time güncellenirken hata: %v", username, dbErr)
				}
			}
			ListenerManager.listeners[username] = listenerInfo
			shouldStartNewListener = !listenerInfo.IsGlobalActive // Eğer veritabanında aktif değilse, başlatılmalı
		}
	} else {
		// Dinleyici bellekte zaten var.
		listenerID = listenerInfo.ListenerDBID
		// Yeni isteğin süresi, genel bitiş süresini uzatıyorsa güncelle.
		if endTimeToAdd.After(listenerInfo.OverallEndTime) {
			listenerInfo.OverallEndTime = endTimeToAdd
			if dbErr := u.repo.UpdateListenerEndTime(listenerID, endTimeToAdd); dbErr != nil {
				log.Printf("'%s' için listener end time güncellenirken hata: %v", username, dbErr)
			}
		}
		shouldStartNewListener = !listenerInfo.IsGlobalActive // Eğer bellekte var ama aktif değilse, yeniden başlatılmalı
	}
	// listenerInfo'nun kendi iç durumunu (UserRequests ve IsGlobalActive) güncelle.
	// Bu kilit, sadece ListenerInfo struct'ının alanlarına erişimi senkronize eder.
	listenerInfo.Lock()
	listenerInfo.UserRequests[currentUserID] = UserRequestInfo{
		UserID:      currentUserID,
		RequestTime: time.Now(),
		EndTime:     endTimeToAdd,
	}
	// Eğer yeni bir dinleyici başlatılacaksa veya zaten aktifse, IsGlobalActive'ı true yap.
	// Bu, startListening'in zaten çalışıp çalışmadığını kontrol etmek için önemlidir.
	listenerInfo.IsGlobalActive = true
	listenerInfo.Unlock()

	// Global ListenerManager kilidini, goroutine başlatmadan önce serbest bırakıyoruz.
	// `defer` zaten bunu hallediyor.

	if shouldStartNewListener {
		log.Printf("'%s' için sohbet dinleme başlatılıyor...", username)
		go u.startListening(listenerInfo) // Yeni goroutine başlat
		return fmt.Sprintf("'%s' kullanıcısının sohbeti dinlenmeye başlandı.", username), nil
	} else {
		log.Printf("'%s' kullanıcısı zaten dinleniyor. Yeni kullanıcı isteği işleniyor.", username)
		return fmt.Sprintf("'%s' kullanıcısının sohbetini dinleme süreniz %s olarak ayarlandı.", username, endTimeToAdd.Format(time.RFC3339)), nil
	}
}

// Geri kalan fonksiyonlar aynı kalıyor...
func getKnownChatId(username string) int {
	knownChatIds := map[string]int{
		// "buraksakinol": 25461130,
	}
	if chatId, exists := knownChatIds[username]; exists {
		return chatId
	}
	return 0
}

var GetChatIdFromKick = func(username string) (int, error) {
	methods := []func(string) (int, error){
		getChatIdFromVercelAPI,
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
	apikey := ""

	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("https://kick-com-api.p.rapidapi.com/channels/%s/chatroom", username)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("istek oluşturulamadı: %w", err)
	}
	req.Header.Add("x-rapidapi-key", apikey)
	req.Header.Add("x-rapidapi-host", "kick-com-api.p.rapidapi.com")
	fmt.Println("4")
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("istek gönderilemedi: %w", err)
	}
	defer resp.Body.Close()
	fmt.Println("3")
	if resp.StatusCode != 200 {
		fmt.Println(resp)
		return 0, fmt.Errorf("RapidAPI isteği başarısız: HTTP %d", resp.StatusCode)
	}
	fmt.Println("2")
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("cevap okunamadı: %w", err)
	}

	var result struct {
		Data struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	fmt.Println("1")
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("JSON parse hatası: %w", err)
	}

	if result.Data.ID == 0 {
		return 0, fmt.Errorf("chat ID bulunamadı (0 döndü)")
	}
	fmt.Println("kullanıcı:id:", result.Data.ID)
	return result.Data.ID, nil
}
func getChatIdFromVercelAPI(username string) (int, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("https://kick-api-provider.vercel.app/api/channel?username=%s", username)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("istek oluşturulamadı: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("istek gönderilemedi: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("istek başarısız: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("cevap okunamadı: %w", err)
	}

	var result struct {
		Chatroom struct {
			ID int `json:"id"`
		} `json:"chatroom"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("JSON parse hatası: %w", err)
	}

	if result.Chatroom.ID == 0 {
		return 0, fmt.Errorf("chat ID bulunamadı (0 döndü)")
	}

	fmt.Println("kullanıcı id:", result.Chatroom.ID)
	return result.Chatroom.ID, nil
}

// GÜNCELLENDİ: unmarshallAndSendToChannel fonksiyonu
func (u *listenUseCase) unmarshallAndSendToChannel(info *ListenerInfo, msgByte []byte) {
	var event Message
	if err := json.Unmarshal(msgByte, &event); err != nil {
		return
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
			select {
			case info.DataChannel <- data:
				// Başarıyla gönderildi
			default:
				log.Printf("'%s' için data channel dolu, mesaj atlanıyor.", info.Username)
			}
			go func(msg []byte) {
				var event Message
				if err := json.Unmarshal(msg, &event); err != nil {
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

					// fmt.Println(data.Content)
					// Linkleri ayıklama ve gösterme
					if linkRegex.MatchString(data.Content) {
						links := linkRegex.FindAllString(data.Content, -1)
						fmt.Print(aurora.Colorize(fmt.Sprintf("💬 %s:%s:%s\n", info.Username, data.Sender.Username, data.Content), utils.GetColorFromHex(data.Sender.Identity.Color))) // utils.GetRandomColorForLog() kullan
						for _, link := range links {
							fmt.Print(aurora.Colorize(fmt.Sprintf("🔗 [%s] %s LINK: %s\n", info.Username, data.Sender.Username, link), aurora.YellowFg|aurora.BoldFm))
						}
					} else {
						fmt.Print(aurora.Colorize(fmt.Sprintf("💬 %s:%s:%s\n", info.Username, data.Sender.Username, data.Content), utils.GetColorFromHex(data.Sender.Identity.Color))) // utils.GetRandomColorForLog() kullan
					}
				}
			}(msgByte)
			// GÜNCELLENDİ: Mesajı veritabanına kaydet
			//go u.saveMessageToDB(info, data)
		}
	}
}

// YENİ: Mesajı veritabanına kaydetme fonksiyonu
func (u *listenUseCase) saveMessageToDB(info *ListenerInfo, data Data) {
	// Link kontrolü yap
	hasLink := linkRegex.MatchString(data.Content)
	var extractedLinks []string

	if hasLink {
		extractedLinks = linkRegex.FindAllString(data.Content, -1)
	}

	// Veritabanına kaydet
	err := u.repo.InsertMessage(info.ListenerDBID, data.Sender.Username, data.Content, data.Timestamp, hasLink, extractedLinks)
	if err != nil {
		log.Printf("'%s' için mesaj veritabanına kaydedilirken hata: %v", info.Username, err)
	} else {
		log.Printf("✅ Mesaj kaydedildi: %s -> %s: %s", info.Username, data.Sender.Username, data.Content)
	}
}
