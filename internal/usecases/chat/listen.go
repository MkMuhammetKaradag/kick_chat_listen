package usecase

import (
	"context"
	"database/sql"
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
	StartActiveListenersOnStartup() error
}

type ListenPostgresRepository interface {
	InsertListener(ctx context.Context, streamerUsername string, kickUserID *int, profilePic *string, userID uuid.UUID, newIsActive bool, newEndTime *time.Time, newDuration int) (uuid.UUID, error)
	// user_id'nin UUID olduğundan emin ol
	InsertUserListenerRequest(listenerID uuid.UUID, userID uuid.UUID, requestTime time.Time, endTime time.Time) error
	// Değiştirildi

	GetStreamerByUsername(ctx context.Context, username string) (*struct {
		ID         uuid.UUID
		KickUserID sql.NullInt32
		ProfilePic sql.NullString
	}, error)

	GetListenerByStreamerIDAndUserID(ctx context.Context, streamerID, userID uuid.UUID) (*struct {
		ID         uuid.UUID
		StreamerID uuid.UUID // Now streamer_id
		UserID     uuid.UUID
		IsActive   bool
		EndTime    *time.Time
		Duration   int
	}, error)
	// Değiştirildi
	GetActiveListeners() ([]domain.ActiveListenerData, error)
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
	UpdateListenerEndTime(ctx context.Context, listenerID uuid.UUID, endTime time.Time) error
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
	DataChannel    chan Data // Mesajları işlemek için

	sync.Mutex // Veya sync.RWMutex kullanabilirsiniz

}

type KickUserInfo struct {
	Chatroom ChatroomInfo `json:"chatroom"`
	User     UserInfo     `json:"user"`
}
type UserInfo struct {
	ID         int    `json:"id"`
	ProfilePic string `json:"profile_pic"`
}
type ChatroomInfo struct {
	ID int `json:"id"`
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
	info, err := GetChatIdFromKick(username)
	if err != nil {
		return 0, err
	}

	return info.Chatroom.ID, nil

}
func (u *listenUseCase) startListening(info *ListenerInfo) {
	// Initial state setup (already exists in your code)
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
		// NOTE: Make sure UpdateListenerStatus accepts context if needed
		if err := u.repo.UpdateListenerStatus(info.ListenerDBID, false); err != nil {
			log.Printf("'%s' için veritabanı listener durumu güncellenirken hata: %v", info.Username, err)
		}

		// ListenerManager'dan bu dinleyiciyi kaldır.
		ListenerManager.Lock()
		delete(ListenerManager.listeners, info.Username)
		ListenerManager.Unlock()

		if info.Client != nil {
			info.Client.Close() // WebSocket bağlantısını kapat
		}
		log.Printf("'%s' için tüm temizlik işlemleri tamamlandı.", info.Username)
	}()

	// --- WebSocket Connection and Subscription ---
	chatId, err := getChatId(info.Username) // Assuming getChatId is defined elsewhere
	if err != nil {
		log.Printf("'%s' için chat ID alınamadı: %v. Dinleme başlatılamıyor.", info.Username, err)
		return
	}
	log.Printf("'%s' için Chat ID: %d bulundu.", info.Username, chatId)

	dialer := websocket.Dialer{}
	wsURL := AppConfig.WebSocketUrl
	subscribeMsg := fmt.Sprintf(AppConfig.ChatroomSubscribeCommand, chatId)

	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		log.Printf("❌ '%s' (ID: %d) için websocket bağlantı hatası: %v", info.Username, chatId, err)
		return
	}
	info.Client = conn
	log.Printf("✅ '%s' (ID: %d) için websocket bağlandı.", info.Username, chatId)

	err = conn.WriteMessage(websocket.TextMessage, []byte(subscribeMsg))
	if err != nil {
		log.Printf("❌ '%s' (ID: %d) için subscribe mesajı gönderme hatası: %v", info.Username, chatId, err)
		return
	}
	log.Printf("✅ '%s' (ID: %d) için subscribe mesajı gönderildi.", info.Username, chatId)

	// --- Message Listening and Processing Loop ---
	// Use a context for graceful shutdown of the ReadMessage goroutine
	readCtx, cancelRead := context.WithCancel(context.Background())
	defer cancelRead() // Ensure cancellation when startListening exits

	// Goroutine to continuously read messages from WebSocket
	go func() {
		for {
			select {
			case <-readCtx.Done():
				log.Printf("'%s' için WebSocket okuma goroutine'i sonlandırılıyor.", info.Username)
				return // Exit this goroutine
			default:
				_, msgByte, readErr := conn.ReadMessage()
				if readErr != nil {
					if websocket.IsCloseError(readErr, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						log.Printf("'%s' için WebSocket bağlantısı normal şekilde kapatıldı.", info.Username)
					} else {
						log.Printf("❌ '%s' için mesaj okuma hatası: %v", info.Username, readErr)
					}
					// If there's a read error, cancel the context to signal main loop to stop
					cancelRead()
					return
				}
				// Pass the raw message bytes to the unmarshaller.
				// This part remains a separate goroutine if unmarshalling is heavy.
				go u.unmarshallAndSendToChannel(info, msgByte)
			}
		}
	}()

	// Main loop to process messages from DataChannel and check listening conditions
	for {
		// Use a ticker for periodic checks (e.g., every second)
		// This prevents the loop from spinning too fast when no messages are coming from WebSocket
		ticker := time.NewTicker(1 * time.Second)
		select {
		case data := <-info.DataChannel: // <-- CONSUME MESSAGES FROM THE CHANNEL HERE!
			// A message was received from the DataChannel, process it.
			// This is where you would typically send it to connected clients
			// or perform further processing.
			// The printing logic that was in unmarshallAndSendToChannel is moved here.
			if linkRegex.MatchString(data.Content) {
				links := linkRegex.FindAllString(data.Content, -1)
				fmt.Print(aurora.Colorize(fmt.Sprintf("💬 %s:%s:%s\n", info.Username, data.Sender.Username, data.Content), utils.GetColorFromHex(data.Sender.Identity.Color)))
				for _, link := range links {
					fmt.Print(aurora.Colorize(fmt.Sprintf("🔗 [%s] %s LINK: %s\n", info.Username, data.Sender.Username, link), aurora.YellowFg|aurora.BoldFm))
				}
			} else {
				fmt.Print(aurora.Colorize(fmt.Sprintf("💬 %s:%s:%s\n", info.Username, data.Sender.Username, data.Content), utils.GetColorFromHex(data.Sender.Identity.Color)))
			}
			// Add any other processing here, e.g., saving to DB
			// go u.saveMessageToDB(info, data) // If you uncomment this, ensure it's safe for concurrency

		case <-ticker.C: // Periodic check
			// Check listening conditions
			info.Lock()
			isActive := info.IsGlobalActive
			overallEndTime := info.OverallEndTime
			numUserRequests := len(info.UserRequests)
			info.Unlock()

			if !isActive || (overallEndTime.Before(time.Now()) && numUserRequests == 0) {
				log.Printf("'%s' için dinleme koşulları artık geçerli değil. Ana döngü kapatılıyor.", info.Username)
				return // Exit the main loop, defer will clean up
			}
		case <-readCtx.Done(): // If the WebSocket reading goroutine signaled an error or closure
			log.Printf("'%s' için WebSocket okuma goroutine'i sonlandığını algıladı. Ana döngü sonlandırılıyor.", info.Username)
			return // Exit the main loop, defer will clean up
		}
		ticker.Stop() // Stop the ticker in each iteration to avoid multiple tickers
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

	// Get Kick user info (kick_user_id and profile_pic)
	kickUserInfo, err := GetChatIdFromKick(username) // Assuming GetChatIdFromKick returns a struct with .User.ID and .User.ProfilePic
	if err != nil {
		return "Sunucu hatası", fmt.Errorf("gönderilen kullanıcının bilgileri alınırken hatayla karşılaşıldı: %w", err)
	}

	if ListenerManager == nil {
		log.Println("FATAL ERROR: ListenerManager başlatılmamış (nil)!")
		return "Sunucu hatası", fmt.Errorf("listener manager başlatılmamış")
	}

	ListenerManager.Lock()
	defer ListenerManager.Unlock()

	listenerInfo, exists := ListenerManager.listeners[username]
	var listenerID uuid.UUID
	shouldStartNewListener := false

	// Try to get streamer from DB first
	streamerData, err := u.repo.GetStreamerByUsername(ctx, username)
	if err != nil {
		log.Printf("Streamer '%s' bilgileri alınırken hata: %v", username, err)
		return "Veritabanı hatası.", err
	}

	var streamerID uuid.UUID
	var kickUserID *int
	var profilePic *string

	if streamerData == nil {
		// Streamer doesn't exist in DB, create it
		log.Printf("Streamer '%s' veritabanında bulunamadı, oluşturuluyor...", username)
		kickUserID = &kickUserInfo.User.ID         // Assuming Kick API returns int ID
		profilePic = &kickUserInfo.User.ProfilePic // Assuming Kick API returns string URL

		// InsertListener handles streamer creation if not exists.
		// So, we just need to ensure we pass kickUserID and profilePic correctly.
		// We'll proceed to the InsertListener call below, which will handle this.
	} else {
		// Streamer exists in DB, use its ID
		streamerID = streamerData.ID
		// Populate kickUserID and profilePic from existing streamerData if needed
		if streamerData.KickUserID.Valid {
			id := int(streamerData.KickUserID.Int32)
			kickUserID = &id
		}
		if streamerData.ProfilePic.Valid {
			pic := streamerData.ProfilePic.String
			profilePic = &pic
		}
	}

	// Check for listener specific to this user and streamer
	// IMPORTANT: Your `listeners` table now has `UNIQUE(streamer_id, user_id)`.
	// So, we should be looking for a listener entry for the specific `currentUserID`
	// and the `streamerID` we just obtained/created.
	dbListenerData, err := u.repo.GetListenerByStreamerIDAndUserID(ctx, streamerID, currentUserID)
	if err != nil {
		log.Printf("'%s' ve '%s' için veritabanı listener kontrol hatası: %v", username, currentUserID, err)
		return "Veritabanı hatası.", err
	}

	if !exists { // Listener for 'username' not in memory (ListenerManager.listeners)
		if dbListenerData == nil {
			// Not in memory, not in DB for this specific user-streamer pair.
			// Create a new listener record and add to memory.
			log.Printf("'%s' kullanıcısı için '%s' dinleyici kaydı bulunamadı, oluşturuluyor...", currentUserID, username)
			newListenerID, insertErr := u.repo.InsertListener(
				ctx,
				username,
				kickUserID, // Pass kickUserID
				profilePic, // Pass profilePic
				currentUserID,
				true, // is_active
				&endTimeToAdd,
				int(durationToAdd.Seconds()),
			)
			if insertErr != nil {
				log.Printf("Veritabanına yeni listener eklenirken hata: %v", insertErr)
				return "Veritabanı hatası.", insertErr
			}
			listenerID = newListenerID
			listenerInfo = &ListenerInfo{
				Username:       username,
				UserRequests:   make(map[uuid.UUID]UserRequestInfo),
				OverallEndTime: endTimeToAdd,
				IsGlobalActive: false, // Will become true when startListening runs
				ListenerDBID:   listenerID,
				DataChannel:    make(chan Data, 1000),
			}
			ListenerManager.listeners[username] = listenerInfo
			shouldStartNewListener = true // New listener needs to be started
		} else {
			// Listener for this user-streamer pair found in DB but not in memory.
			// Reload it into memory.
			listenerID = dbListenerData.ID
			listenerInfo = &ListenerInfo{
				Username:       username,
				UserRequests:   make(map[uuid.UUID]UserRequestInfo),
				OverallEndTime: *dbListenerData.EndTime,
				IsGlobalActive: dbListenerData.IsActive,
				ListenerDBID:   listenerID,
				DataChannel:    make(chan Data, 100),
			}

			// Load existing user requests for this listener if any
			if userRequests, reqErr := u.repo.GetUserRequestsForListener(listenerID); reqErr == nil {
				for _, req := range userRequests {
					listenerInfo.UserRequests[req.UserID] = UserRequestInfo{
						UserID:      req.UserID,
						RequestTime: req.RequestTime,
						EndTime:     req.EndTime,
					}
				}
			} else {
				log.Printf("Error loading user requests for listener %s: %v", listenerID, reqErr)
			}

			// Update overall end time if new request extends it
			if endTimeToAdd.After(listenerInfo.OverallEndTime) {
				listenerInfo.OverallEndTime = endTimeToAdd
				if dbErr := u.repo.UpdateListenerEndTime(ctx, listenerID, endTimeToAdd); dbErr != nil {
					log.Printf("'%s' için listener end time güncellenirken hata: %v", username, dbErr)
				}
			}
			ListenerManager.listeners[username] = listenerInfo
			shouldStartNewListener = !listenerInfo.IsGlobalActive // If not active, start it
		}
	} else {
		// Listener already in memory.
		listenerID = listenerInfo.ListenerDBID
		// Update overall end time if new request extends it
		if endTimeToAdd.After(listenerInfo.OverallEndTime) {
			listenerInfo.OverallEndTime = endTimeToAdd
			if dbErr := u.repo.UpdateListenerEndTime(ctx, listenerID, endTimeToAdd); dbErr != nil {
				log.Printf("'%s' için listener end time güncellenirken hata: %v", username, dbErr)
			}
		}
		shouldStartNewListener = !listenerInfo.IsGlobalActive // If in memory but not active, restart
	}

	// Update the specific user's request within this listener's info
	listenerInfo.Lock() // Assuming ListenerInfo has its own mutex
	listenerInfo.UserRequests[currentUserID] = UserRequestInfo{
		UserID:      currentUserID,
		RequestTime: time.Now(),
		EndTime:     endTimeToAdd,
	}
	listenerInfo.IsGlobalActive = true // If a request comes in, it implies it should be active
	listenerInfo.Unlock()

	// Global ListenerManager lock is released by defer at the top.

	if shouldStartNewListener {
		log.Printf("'%s' için sohbet dinleme başlatılıyor...", username)
		go u.startListening(listenerInfo) // Start new goroutine
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

var GetChatIdFromKick = func(username string) (*KickUserInfo, error) {
	methods := []func(string) (*KickUserInfo, error){
		getChatIdFromVercelAPI,
	}
	for _, method := range methods {
		chatId, err := method(username)
		if err == nil {
			return chatId, nil
		}
	}

	return nil, fmt.Errorf("tüm yöntemler başarısız")
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
func getChatIdFromVercelAPI(username string) (*KickUserInfo, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("https://kick-api-provider.vercel.app/api/channel?username=%s", username)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("istek oluşturulamadı: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("istek gönderilemedi: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("istek başarısız: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cevap okunamadı: %w", err)
	}

	var result KickUserInfo

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("JSON parse hatası: %w", err)
	}

	if result.Chatroom.ID == 0 {
		return nil, fmt.Errorf("chat ID bulunamadı (0 döndü)")
	}

	fmt.Println("kullanıcı id:", result.Chatroom.ID)
	return &result, nil
}

func (u *listenUseCase) unmarshallAndSendToChannel(info *ListenerInfo, msgByte []byte) {
	var event Message
	if err := json.Unmarshal(msgByte, &event); err != nil {
		log.Printf("JSON unmarshal event hatası: %v", err)
		return
	}

	if event.Event == "App\\Events\\ChatMessageEvent" {
		var rawDataString string
		if err := json.Unmarshal(event.Data, &rawDataString); err != nil {
			log.Printf("JSON unmarshal rawDataString hatası: %v", err)
			return
		}

		var data Data
		if err := json.Unmarshal([]byte(rawDataString), &data); err != nil {
			log.Printf("JSON unmarshal data hatası: %v", err)
			return
		}

		if data.Type == "message" {
			select {
			case info.DataChannel <- data:
				// Başarıyla gönderildi
			default:
				log.Printf("'%s' için data channel dolu, mesaj atlanıyor.", info.Username)
			}
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
