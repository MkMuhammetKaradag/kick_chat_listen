package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"kick-chat/domain"
	"kick-chat/internal/middleware"
	"log"
	"net/http"
	"regexp"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
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

func (u *listenUseCase) Execute(fbrCtx *fiber.Ctx, ctx context.Context, username string) (string, error) {
	userData, ok := middleware.GetUserData(fbrCtx)
	if !ok {
		return "", domain.ErrNotFoundAuthorization

	}
	currentUserID, err := uuid.Parse(userData.UserID)
	if err != nil {
		return "", domain.ErrNotFoundAuthorization

	}
	fmt.Println("currentuserid:", userData.UserID)

	durationToAdd := 7 * 24 * time.Hour
	endTimeToAdd := time.Now().Add(durationToAdd)

	if ListenerManager == nil {
		log.Println("FATAL ERROR: ListenerManager başlatılmamış (nil)!")
		return "Sunucu hatası", fmt.Errorf("listener manager başlatılmamış")
	}

	ListenerManager.Lock()
	listenerInfo, exists := ListenerManager.listeners[username]
	ListenerManager.Unlock()

	var listenerID uuid.UUID

	if !exists || !listenerInfo.IsGlobalActive {
		log.Printf("'%s' için yeni listener süreci başlatılıyor...", username)

		dbListenerData, err := u.repo.GetListenerByStreamerUsername(username)
		if err != nil {
			log.Printf("'%s' için veritabanı kontrol hatası: %v", username, err)
			return "veritabanı hatası", err
		}

		if dbListenerData == nil {
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
				IsGlobalActive: true,
				ListenerDBID:   listenerID,
			}
			ListenerManager.Lock()
			ListenerManager.listeners[username] = listenerInfo
			ListenerManager.Unlock()

		} else {
			listenerID = dbListenerData.ID
			ListenerManager.Lock()
			if existingListenerInfo, ok := ListenerManager.listeners[username]; ok {
				if endTimeToAdd.After(existingListenerInfo.OverallEndTime) {
					existingListenerInfo.OverallEndTime = endTimeToAdd
					// Veritabanını güncelle
					if dbErr := u.repo.UpdateListenerEndTime(listenerID, endTimeToAdd); dbErr != nil {
						log.Printf("'%s' için listener end time güncellenirken hata: %v", username, dbErr)
					}
				}
				listenerInfo = existingListenerInfo
			} else {
				listenerInfo = &ListenerInfo{
					Username:       username,
					UserRequests:   make(map[uuid.UUID]UserRequestInfo),
					OverallEndTime: *dbListenerData.EndTime,
					IsGlobalActive: dbListenerData.IsActive,
					ListenerDBID:   listenerID,
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

				if endTimeToAdd.After(listenerInfo.OverallEndTime) {
					listenerInfo.OverallEndTime = endTimeToAdd
					if dbErr := u.repo.UpdateListenerEndTime(listenerID, endTimeToAdd); dbErr != nil {
						log.Printf("'%s' için listener end time güncellenirken hata: %v", username, dbErr)
					}
				}

				ListenerManager.listeners[username] = listenerInfo
			}
			ListenerManager.Unlock()
		}

		err = u.repo.InsertUserListenerRequest(listenerID, currentUserID, time.Now(), endTimeToAdd)
		if err != nil {
			log.Printf("Veritabanına yeni user listener request eklenirken hata: %v", err)
		} else {
			listenerInfo.Lock()
			listenerInfo.UserRequests[currentUserID] = UserRequestInfo{
				UserID:      currentUserID,
				RequestTime: time.Now(),
				EndTime:     endTimeToAdd,
			}
			if endTimeToAdd.After(listenerInfo.OverallEndTime) {
				listenerInfo.OverallEndTime = endTimeToAdd
				if dbErr := u.repo.UpdateListenerEndTime(listenerID, endTimeToAdd); dbErr != nil {
					log.Printf("'%s' için listener end time güncellenirken hata: %v", username, dbErr)
				}
			}
			listenerInfo.Unlock()
		}

		if listenerInfo.IsGlobalActive {
			log.Printf("'%s' için sohbet dinleme başlatılıyor...", username)
			newDataChannel := make(chan Data, 100)
			listenerInfo.DataChannel = newDataChannel

			// Repository'yi de startListening fonksiyonuna geçmemiz gerekiyor
			go u.startListening(listenerInfo)
		}

		return fmt.Sprintf("'%s' kullanıcısının sohbeti dinlenmeye başlandı.", username), nil

	} else {
		log.Printf("'%s' kullanıcısı zaten dinleniyor. Yeni kullanıcı isteği işleniyor.", username)

		err := u.repo.InsertUserListenerRequest(listenerInfo.ListenerDBID, currentUserID, time.Now(), endTimeToAdd)
		if err != nil {
			log.Printf("Mevcut listener için veritabanına yeni user listener request eklenirken hata: %v", err)
			return "Veritabanı hatası.", err
		}

		listenerInfo.Lock()
		listenerInfo.UserRequests[currentUserID] = UserRequestInfo{
			UserID:      currentUserID,
			RequestTime: time.Now(),
			EndTime:     endTimeToAdd,
		}
		if endTimeToAdd.After(listenerInfo.OverallEndTime) {
			listenerInfo.OverallEndTime = endTimeToAdd
			if dbErr := u.repo.UpdateListenerEndTime(listenerInfo.ListenerDBID, endTimeToAdd); dbErr != nil {
				log.Printf("'%s' için listener end time güncellenirken hata: %v", username, dbErr)
			}
		}
		listenerInfo.Unlock()

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
		fmt.Println("rawDataString:", rawDataString)
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

			// GÜNCELLENDİ: Mesajı veritabanına kaydet
			go u.saveMessageToDB(info, data)
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
