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
	"github.com/gorilla/websocket"
	"github.com/logrusorgru/aurora"
)

// Configuration
type Config struct {
	WebSocketUrl             string
	ChatroomSubscribeCommand string
	BatchSize                int
	MessageBufferSize        int
	ReconnectInterval        time.Duration
	MaxReconnectAttempts     int
}

var AppConfig = &Config{
	BatchSize:                10,
	ChatroomSubscribeCommand: "{\"event\":\"pusher:subscribe\",\"data\":{\"auth\":\"\",\"channel\":\"chatrooms.%d.v2\"}}",
	WebSocketUrl:             "wss://ws-us2.pusher.com/app/32cbd69e4b950bf97679?protocol=7&client=js&version=8.4.0&flash=false",
	MessageBufferSize:        1000,
	ReconnectInterval:        5 * time.Second,
	MaxReconnectAttempts:     3,
}

// Link regex compiled once
var linkRegex = regexp.MustCompile(`https?://[^\s]+`)

// Domain Models
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

// Enhanced UserRequestInfo with validation
type UserRequestInfo struct {
	UserID      uuid.UUID `json:"user_id"`
	RequestTime time.Time `json:"request_time"`
	EndTime     time.Time `json:"end_time"`
}

func (u *UserRequestInfo) IsActive() bool {
	return time.Now().Before(u.EndTime)
}

// Enhanced ListenerInfo with better state management
type ListenerInfo struct {
	Username          string                        `json:"username"`
	Client            *websocket.Conn               `json:"-"`
	UserRequests      map[uuid.UUID]UserRequestInfo `json:"user_requests"`
	OverallEndTime    time.Time                     `json:"overall_end_time"`
	IsGlobalActive    bool                          `json:"is_global_active"`
	ListenerDBID      uuid.UUID                     `json:"listener_db_id"`
	DataChannel       chan Data                     `json:"-"`
	StopChannel       chan struct{}                 `json:"-"`
	ReconnectAttempts int                           `json:"reconnect_attempts"`
	LastActivity      time.Time                     `json:"last_activity"`

	mu sync.RWMutex
}

// Thread-safe methods for ListenerInfo
func (l *ListenerInfo) SetActive(active bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.IsGlobalActive = active
	l.LastActivity = time.Now()
}

func (l *ListenerInfo) IsActive() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.IsGlobalActive
}

func (l *ListenerInfo) AddUserRequest(userID uuid.UUID, endTime time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.UserRequests[userID] = UserRequestInfo{
		UserID:      userID,
		RequestTime: time.Now(),
		EndTime:     endTime,
	}

	// Update overall end time if necessary
	if endTime.After(l.OverallEndTime) {
		l.OverallEndTime = endTime
	}
	l.LastActivity = time.Now()
}

func (l *ListenerInfo) RemoveExpiredRequests() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	for userID, request := range l.UserRequests {
		if now.After(request.EndTime) {
			delete(l.UserRequests, userID)
		}
	}
}

func (l *ListenerInfo) HasActiveRequests() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	now := time.Now()
	for _, request := range l.UserRequests {
		if now.Before(request.EndTime) {
			return true
		}
	}
	return false
}

func (l *ListenerInfo) GetActiveRequestCount() int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	count := 0
	now := time.Now()
	for _, request := range l.UserRequests {
		if now.Before(request.EndTime) {
			count++
		}
	}
	return count
}

// Enhanced ListenerManager with better concurrency
type ListenerManagerType struct {
	listeners map[string]*ListenerInfo
	mu        sync.RWMutex
}

func (lm *ListenerManagerType) GetListener(username string) (*ListenerInfo, bool) {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	listener, exists := lm.listeners[username]
	return listener, exists
}

func (lm *ListenerManagerType) AddListener(username string, info *ListenerInfo) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.listeners[username] = info
}

func (lm *ListenerManagerType) RemoveListener(username string) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	if listener, exists := lm.listeners[username]; exists {
		// Close channels safely
		select {
		case <-listener.StopChannel:
		default:
			close(listener.StopChannel)
		}
		delete(lm.listeners, username)
	}
}

func (lm *ListenerManagerType) GetActiveListenerCount() int {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	count := 0
	for _, listener := range lm.listeners {
		if listener.IsActive() {
			count++
		}
	}
	return count
}

// Cleanup expired listeners periodically
func (lm *ListenerManagerType) StartCleanupRoutine(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lm.cleanupExpiredListeners()
		}
	}
}

func (lm *ListenerManagerType) cleanupExpiredListeners() {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	now := time.Now()
	fmt.Println("now:", now)
	for username, listener := range lm.listeners {
		// Remove expired requests first
		listener.RemoveExpiredRequests()

		// If no active requests and listener is inactive, remove it
		if !listener.HasActiveRequests() && !listener.IsActive() {
			log.Printf("Cleaning up expired listener for '%s'", username)
			select {
			case <-listener.StopChannel:
			default:
				close(listener.StopChannel)
			}
			delete(lm.listeners, username)
		}
	}
}

// Global ListenerManager
var ListenerManager = NewListenerManager()

func NewListenerManager() *ListenerManagerType {
	return &ListenerManagerType{
		listeners: make(map[string]*ListenerInfo),
	}
}

// Repository interface remains the same
type ListenPostgresRepository interface {
	InsertListener(ctx context.Context, streamerUsername string, kickUserID *int, profilePic *string, userID uuid.UUID, newIsActive bool, newEndTime *time.Time, newDuration int) (uuid.UUID, error)
	InsertUserListenerRequest(listenerID uuid.UUID, userID uuid.UUID, requestTime time.Time, endTime time.Time) error
	GetStreamerByUsername(ctx context.Context, username string) (*struct {
		ID         uuid.UUID
		KickUserID sql.NullInt32
		ProfilePic sql.NullString
	}, error)
	GetListenerByStreamerIDAndUserID(ctx context.Context, streamerID, userID uuid.UUID) (*struct {
		ID         uuid.UUID
		StreamerID uuid.UUID
		UserID     uuid.UUID
		IsActive   bool
		EndTime    *time.Time
		Duration   int
	}, error)
	GetActiveListeners() ([]domain.ActiveListenerData, error)
	GetUserRequestsForListener(listenerID uuid.UUID) ([]struct {
		UserID      uuid.UUID
		RequestTime time.Time
		EndTime     time.Time
	}, error)
	InsertMessage(listenerID uuid.UUID, senderUsername, content string, timestamp time.Time, hasLink bool, extractedLinks []string) error
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

// UseCase interface and implementation
type ListenUseCase interface {
	Execute(fbrCtx *fiber.Ctx, ctx context.Context, username string) (string, error)
	StartActiveListenersOnStartup() error
	StopListener(username string) error
	GetListenerStats() map[string]interface{}
}

type listenUseCase struct {
	repo   ListenPostgresRepository
	config *Config
}

func NewListenUseCase(repo ListenPostgresRepository) ListenUseCase {
	return &listenUseCase{
		repo:   repo,
		config: AppConfig,
	}
}

// Enhanced Execute method with better error handling
func (u *listenUseCase) Execute(fbrCtx *fiber.Ctx, ctx context.Context, username string) (string, error) {
	// Validate input
	if username == "" {
		return "", fmt.Errorf("username cannot be empty")
	}

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

	// Get Kick user info with timeout context
	kickCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	kickUserInfo, err := u.getChatIdFromKickWithContext(kickCtx, username)
	if err != nil {
		return "Sunucu hatası", fmt.Errorf("kullanıcı bilgileri alınamadı: %w", err)
	}

	// Handle listener logic
	listenerInfo, exists := ListenerManager.GetListener(username)

	if !exists {
		// Create new listener
		return u.createNewListener(ctx, username, currentUserID, endTimeToAdd, durationToAdd, kickUserInfo)
	}

	// Update existing listener
	return u.updateExistingListener(ctx, listenerInfo, currentUserID, endTimeToAdd, username)
}

func (u *listenUseCase) createNewListener(ctx context.Context, username string, userID uuid.UUID, endTime time.Time, duration time.Duration, kickInfo *KickUserInfo) (string, error) {
	kickUserID := &kickInfo.User.ID
	profilePic := &kickInfo.User.ProfilePic

	listenerID, err := u.repo.InsertListener(
		ctx,
		username,
		kickUserID,
		profilePic,
		userID,
		true,
		&endTime,
		int(duration.Seconds()),
	)
	if err != nil {
		return "Veritabanı hatası", fmt.Errorf("listener oluşturulamadı: %w", err)
	}

	listenerInfo := &ListenerInfo{
		Username:       username,
		UserRequests:   make(map[uuid.UUID]UserRequestInfo),
		OverallEndTime: endTime,
		ListenerDBID:   listenerID,
		DataChannel:    make(chan Data, u.config.MessageBufferSize),
		StopChannel:    make(chan struct{}),
		LastActivity:   time.Now(),
	}

	listenerInfo.AddUserRequest(userID, endTime)
	ListenerManager.AddListener(username, listenerInfo)

	go u.startListening(listenerInfo)

	return fmt.Sprintf("'%s' kullanıcısının sohbeti dinlenmeye başlandı", username), nil
}

func (u *listenUseCase) updateExistingListener(ctx context.Context, listenerInfo *ListenerInfo, userID uuid.UUID, endTime time.Time, username string) (string, error) {
	listenerInfo.AddUserRequest(userID, endTime)

	// Update DB if necessary
	if endTime.After(listenerInfo.OverallEndTime) {
		if err := u.repo.UpdateListenerEndTime(ctx, listenerInfo.ListenerDBID, endTime); err != nil {
			log.Printf("Listener end time güncellenirken hata: %v", err)
		}
	}

	// Restart if not active
	if !listenerInfo.IsActive() {
		go u.startListening(listenerInfo)
		return fmt.Sprintf("'%s' kullanıcısının sohbeti yeniden başlatıldı", username), nil
	}

	return fmt.Sprintf("'%s' kullanıcısının sohbet dinleme süresi güncellendi", username), nil
}

// Enhanced startListening with better error handling and reconnection
func (u *listenUseCase) startListening(info *ListenerInfo) {
	defer u.cleanupListener(info)

	info.SetActive(true)
	log.Printf("'%s' için sohbet dinleme başlatılıyor", info.Username)

	for {
		select {
		case <-info.StopChannel:
			log.Printf("'%s' için stop signal alındı", info.Username)
			return
		default:
			if err := u.runListeningLoop(info); err != nil {
				log.Printf("'%s' için listening loop hatası: %v", info.Username, err)

				if info.ReconnectAttempts >= u.config.MaxReconnectAttempts {
					log.Printf("'%s' için maksimum reconnect denemesi aşıldı", info.Username)
					return
				}

				info.ReconnectAttempts++
				time.Sleep(u.config.ReconnectInterval)
				continue
			}
			return
		}
	}
}

func (u *listenUseCase) runListeningLoop(info *ListenerInfo) error {
	chatId, err := u.getChatId(info.Username)
	if err != nil {
		return fmt.Errorf("chat ID alınamadı: %w", err)
	}

	conn, err := u.connectWebSocket(chatId)
	if err != nil {
		return fmt.Errorf("websocket bağlantısı kurulamadı: %w", err)
	}
	defer conn.Close()

	info.Client = conn
	info.ReconnectAttempts = 0 // Reset on successful connection

	// Start message reading goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go u.readMessages(ctx, info, conn)

	// Main processing loop
	return u.processMessages(info, cancel)
}

func (u *listenUseCase) connectWebSocket(chatId int) (*websocket.Conn, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(u.config.WebSocketUrl, nil)
	if err != nil {
		return nil, err
	}

	subscribeMsg := fmt.Sprintf(u.config.ChatroomSubscribeCommand, chatId)
	if err := conn.WriteMessage(websocket.TextMessage, []byte(subscribeMsg)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("subscribe mesajı gönderilemedi: %w", err)
	}

	return conn, nil
}

func (u *listenUseCase) readMessages(ctx context.Context, info *ListenerInfo, conn *websocket.Conn) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			_, msgByte, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					log.Printf("'%s' için WebSocket normal şekilde kapatıldı", info.Username)
				} else {
					log.Printf("'%s' için mesaj okuma hatası: %v", info.Username, err)
				}
				return
			}

			go u.unmarshallAndSendToChannel(info, msgByte)
		}
	}
}

func (u *listenUseCase) processMessages(info *ListenerInfo, cancel context.CancelFunc) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case data := <-info.DataChannel:
			u.handleMessage(info, data)

		case <-ticker.C:
			if !u.shouldContinueListening(info) {
				cancel()
				return nil
			}

		case <-info.StopChannel:
			cancel()
			return nil
		}
	}
}

func (u *listenUseCase) shouldContinueListening(info *ListenerInfo) bool {
	if !info.IsActive() {
		return false
	}

	info.RemoveExpiredRequests()
	return info.HasActiveRequests() || time.Now().Before(info.OverallEndTime)
}

func (u *listenUseCase) handleMessage(info *ListenerInfo, data Data) {
	// Display message with color
	if linkRegex.MatchString(data.Content) {
		links := linkRegex.FindAllString(data.Content, -1)
		fmt.Print(aurora.Colorize(
			fmt.Sprintf("💬 %s:%s:%s\n", info.Username, data.Sender.Username, data.Content),
			utils.GetColorFromHex(data.Sender.Identity.Color),
		))
		for _, link := range links {
			fmt.Print(aurora.Colorize(
				fmt.Sprintf("🔗 [%s] %s LINK: %s\n", info.Username, data.Sender.Username, link),
				aurora.YellowFg|aurora.BoldFm,
			))
		}
	} else {
		fmt.Print(aurora.Colorize(
			fmt.Sprintf("💬 %s:%s:%s\n", info.Username, data.Sender.Username, data.Content),
			utils.GetColorFromHex(data.Sender.Identity.Color),
		))
	}

	// Save to database asynchronously
	// go u.saveMessageToDB(info, data)
}

func (u *listenUseCase) cleanupListener(info *ListenerInfo) {
	log.Printf("'%s' için cleanup başlatılıyor", info.Username)

	info.SetActive(false)

	if err := u.repo.UpdateListenerStatus(info.ListenerDBID, false); err != nil {
		log.Printf("'%s' için veritabanı durumu güncellenirken hata: %v", info.Username, err)
	}

	ListenerManager.RemoveListener(info.Username)

	if info.Client != nil {
		info.Client.Close()
	}

	log.Printf("'%s' için cleanup tamamlandı", info.Username)
}

// API helper methods with better error handling
func (u *listenUseCase) getChatId(username string) (int, error) {
	if knownId := getKnownChatId(username); knownId != 0 {
		return knownId, nil
	}

	info, err := GetChatIdFromKick(username)
	if err != nil {
		return 0, err
	}

	return info.Chatroom.ID, nil
}

func (u *listenUseCase) getChatIdFromKickWithContext(ctx context.Context, username string) (*KickUserInfo, error) {
	type result struct {
		info *KickUserInfo
		err  error
	}

	ch := make(chan result, 1)
	go func() {
		info, err := GetChatIdFromKick(username)
		ch <- result{info, err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		return res.info, res.err
	}
}

// Additional utility methods
func (u *listenUseCase) StopListener(username string) error {
	listenerInfo, exists := ListenerManager.GetListener(username)
	if !exists {
		return fmt.Errorf("listener not found for username: %s", username)
	}

	select {
	case listenerInfo.StopChannel <- struct{}{}:
		return nil
	default:
		return fmt.Errorf("failed to send stop signal")
	}
}

func (u *listenUseCase) GetListenerStats() map[string]interface{} {
	return map[string]interface{}{
		"active_listeners": ListenerManager.GetActiveListenerCount(),
		"total_listeners":  len(ListenerManager.listeners),
	}
}



// Message processing
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
				// Successfully sent
			default:
				log.Printf("'%s' için data channel dolu, mesaj atlanıyor", info.Username)
			}
		}
	}
}

func (u *listenUseCase) saveMessageToDB(info *ListenerInfo, data Data) {
	hasLink := linkRegex.MatchString(data.Content)
	var extractedLinks []string

	if hasLink {
		extractedLinks = linkRegex.FindAllString(data.Content, -1)
	}

	err := u.repo.InsertMessage(info.ListenerDBID, data.Sender.Username, data.Content, data.Timestamp, hasLink, extractedLinks)
	if err != nil {
		log.Printf("'%s' için mesaj veritabanına kaydedilirken hata: %v", info.Username, err)
	}
}

// Helper functions (kept same for compatibility)
func getKnownChatId(username string) int {
	knownChatIds := map[string]int{
		// Add known chat IDs here
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

	return &result, nil
}
