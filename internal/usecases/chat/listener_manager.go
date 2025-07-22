package usecase // Veya ana paketinize göre ayarlanmalı

import (
	"encoding/json"
	"fmt"
	"kick-chat/utils"
	"log"
	"sync"
	"time"

	// Gerekli importlar
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/logrusorgru/aurora"
)

// --- Struct Tanımları ---
// Config struct'ı (eğer usecase içinde kullanılıyorsa)
type Config struct {
	WebSocketUrl             string
	ChatroomSubscribeCommand string
	BatchSize                int
}

type UserRequestInfo struct {
	UserID      string
	RequestTime time.Time
	EndTime     time.Time
}

type ListenerInfo struct {
	Username       string
	Client         *websocket.Conn
	UserRequests   map[string]UserRequestInfo
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
	defer func() {
		log.Printf("'%s' için sohbet dinleme goroutine'i sonlanıyor.", info.Username)
		// ListenerManager global olduğu için ListenerManager'a erişebiliriz
		ListenerManager.Lock()
		delete(ListenerManager.listeners, info.Username)
		ListenerManager.Unlock()

		// Veritabanı güncelleme (is_active = false) için repo'yu kullanın
		// Bu kısım için ListenerInfo'ya repo'yu eklemek veya global repo kullanmak gerekebilir.
		// Örneğin: u.repo.UpdateListenerStatus(info.ListenerDBID, false)
	}()

	// ... (Chat ID alma mantığı) ...
	// Chat ID alma kısımında RapidAPI veya manuel giriş kullanılmalı.

	// Bu kısım çok önemli: Chat ID'yi doğru alın.
	chatId, err := getChatId(info.Username) // getChatId hem bilinen ID'leri hem de manuel/API'yi kapsayan bir fonksiyon olmalı
	if err != nil {
		log.Printf("'%s' için chat ID alınamadı: %v. Dinleme başlatılamıyor.", info.Username, err)
		return
	}
	log.Printf("'%s' için Chat ID: %d bulundu.", info.Username, chatId)

	// WebSocket bağlantısı kurma
	dialer := websocket.Dialer{}
	wsURL := AppConfig.WebSocketUrl
	subscribeMsg := fmt.Sprintf(AppConfig.ChatroomSubscribeCommand, chatId)

	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		log.Printf("❌ '%s' (ID: %d) için websocket bağlantı hatası: %v", info.Username, chatId, err)
		// Yeniden deneme mekanizması ekleyin
		return
	}
	info.Client = conn
	log.Printf("✅ '%s' (ID: %d) için websocket bağlandı.", info.Username, chatId)

	err = conn.WriteMessage(websocket.TextMessage, []byte(subscribeMsg))
	if err != nil {
		log.Printf("❌ '%s' (ID: %d) için subscribe mesajı gönderme hatası: %v", info.Username, chatId, err)
		conn.Close()
		return
	}
	log.Printf("✅ '%s' (ID: %d) için subscribe mesajı gönderildi.", info.Username, chatId)
	// 3. Mesaj dinleme döngüsünü başlat
	go func() {
		defer func() {
			log.Printf("'%s' için mesaj okuma döngüsü sonlanıyor.", info.Username)
			if info.Client != nil {
				info.Client.Close() // Bağlantıyı kapat
			}
			// Gerekirse ListenerManager'dan temizlik yap
		}()

		for {
			// Bağlantı kapanmışsa veya süresi dolmuşsa döngüden çık
			if !info.IsGlobalActive || (info.OverallEndTime.Before(time.Now()) && len(info.UserRequests) == 0) {
				log.Printf("'%s' için dinleme koşulları artık geçerli değil.", info.Username)
				break
			}

			_, msgByte, readErr := conn.ReadMessage()
			if readErr != nil {
				log.Printf("❌ '%s' için mesaj okuma hatası: %v", info.Username, readErr)
				// Bağlantı hatası durumunda yeniden bağlanmayı denemeliyiz.
				// Bu kısım için retry mekanizması eklenmeli.
				break // Şimdilik döngüden çık
			}

			// Mesajı işle ve dataChannel'a gönder

			go u.unmarshallAndSendToChannel(info, msgByte)

			// Sohbet mesajını ayrıştırıp veritabanına kaydetme
			// (Bu işi ayrı bir goroutine'de yapabiliriz)
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
						fmt.Print(aurora.Colorize(fmt.Sprintf("💬 %s:%s:%s\n", info.Username, data.Sender.Username, data.Content), utils.GetRandomColorForLog())) // utils.GetRandomColorForLog() kullan
						for _, link := range links {
							fmt.Print(aurora.Colorize(fmt.Sprintf("🔗 [%s] %s LINK: %s\n", info.Username, data.Sender.Username, link), aurora.YellowFg|aurora.BoldFm))
						}
					} else {
						fmt.Print(aurora.Colorize(fmt.Sprintf("💬 %s:%s:%s\n", info.Username, data.Sender.Username, data.Content), utils.GetRandomColorForLog())) // utils.GetRandomColorForLog() kullan
					}
				}
			}(msgByte)
		}
	}()
}

// NewListenerManager constructor
func NewListenerManager() *ListenerManagerType {
	return &ListenerManagerType{
		listeners: make(map[string]*ListenerInfo),
	}
}
