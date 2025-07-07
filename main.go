package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

const batchSize = 10
const webSocketUrl string = "wss://ws-us2.pusher.com/app/32cbd69e4b950bf97679?protocol=7&client=js&version=8.4.0&flash=false"
const chatroomSubcribeCommand string = "{\"event\":\"pusher:subscribe\",\"data\":{\"auth\":\"\",\"channel\":\"chatrooms.%d.v2\"}}"

// Global maps
var ChannelAndChatIdMap = make(map[string]int)
var ChatIdAndChannelMap = make(map[int]string)

// Link detection regex
var linkRegex = regexp.MustCompile(`https?://[^\s]+`)

// Message structs (bunları projenizde tanımlamalısınız)
// type Message struct {
// 	Event string          `json:"event"`
// 	Data  json.RawMessage `json:"data"`
// }

// type Data struct {
// 	Type       string    `json:"type"`
// 	Content    string    `json:"content"`
// 	ChatroomID int       `json:"chatroom_id"`
// 	CreatedAt  time.Time `json:"created_at"`
// 	Sender     struct {
// 		Username string `json:"username"`
// 	} `json:"sender"`
// }

func main() {
	rand.New(rand.NewSource(time.Now().UnixNano()))

	// Kullanıcı adlarını al
	usernames := getUsernames()
	if len(usernames) == 0 {
		color.Red("❌ Hiç kullanıcı adı girilmedi!")
		return
	}

	// Chat ID'lerini al
	err := fetchChatIds(usernames)
	if err != nil {
		color.Red("❌ Chat ID'leri alınırken hata: %v", err)
		return
	}

	// Başarılı bağlantıları göster
	color.Green("✅ Dinlenecek kanallar:")
	for username, chatId := range ChannelAndChatIdMap {
		color.Cyan("   • %s (ID: %d)", username, chatId)
	}
	fmt.Println()

	dataChannel := make(chan Data, 100)
	go HandleDataChannelInserts(dataChannel)

	// Her kanal için WebSocket bağlantısı başlat
	for channelName, chatRoomId := range ChannelAndChatIdMap {
		go startListeningChat(channelName, chatRoomId, dataChannel)
	}

	// Main goroutine'i blokla
	select {}
}

func getUsernames() []string {
	reader := bufio.NewReader(os.Stdin)

	color.Yellow("🎯 Kick Chat Dinleyici")
	color.White("Dinlemek istediğiniz kullanıcı adlarını girin:")
	color.White("- Tek kullanıcı: username")
	color.White("- Çoklu kullanıcı: username1,username2,username3")
	color.White("- Veya her satırda bir kullanıcı adı yazabilirsiniz (boş satır ile bitirin)")
	fmt.Print("\n> ")

	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	var usernames []string

	// Virgülle ayrılmış kontrol et
	if strings.Contains(input, ",") {
		usernames = strings.Split(input, ",")
		for i, username := range usernames {
			usernames[i] = strings.TrimSpace(username)
		}
	} else if input != "" {
		// Tek kullanıcı veya çoklu satır girişi
		usernames = append(usernames, input)

		// Çoklu satır girişi için devam et
		for {
			fmt.Print("> ")
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(input)

			if input == "" {
				break
			}
			usernames = append(usernames, input)
		}
	}

	// Boş username'leri filtrele
	var filteredUsernames []string
	for _, username := range usernames {
		if username != "" {
			filteredUsernames = append(filteredUsernames, username)
		}
	}

	return filteredUsernames
}

func fetchChatIds(usernames []string) error {
	color.Yellow("🔍 Chat ID'leri alınıyor...")
	color.Yellow("⚠️ Kick.com Cloudflare koruması kullanıyor - manuel ID girişi önerilir")

	for _, username := range usernames {
		chatId, err := getChatIdFromKick(username)
		if err != nil {
			color.Red("❌ %s için chat ID alınamadı: %v", username, err)

			// Bilinen chat ID'lerini kontrol et
			if knownId := getKnownChatId(username); knownId != 0 {
				color.Green("✅ %s için bilinen chat ID kullanılıyor: %d", username, knownId)
				ChannelAndChatIdMap[username] = knownId
				ChatIdAndChannelMap[knownId] = username
				continue
			}

			// Manuel giriş seçeneği
			if manualId := askForManualChatId(username); manualId != 0 {
				ChannelAndChatIdMap[username] = manualId
				ChatIdAndChannelMap[manualId] = username
				color.Green("✅ %s -> %d (manuel)", username, manualId)
				continue
			}

			color.Red("❌ %s atlanıyor", username)
			continue
		}

		ChannelAndChatIdMap[username] = chatId
		ChatIdAndChannelMap[chatId] = username
		color.Green("✅ %s -> %d", username, chatId)
	}

	if len(ChannelAndChatIdMap) == 0 {
		return fmt.Errorf("hiçbir kullanıcı için chat ID alınamadı")
	}

	return nil
}

func getChatIdFromKick(username string) (int, error) {
	// Kick web sayfasından chat ID almayı dene
	methods := []func(string) (int, error){
		getChatIdFromWebPage,
		getChatIdFromAPIv2,
		getChatIdFromRapidAPI,
		getChatIdFromAlternativeAPI,
	}

	for i, method := range methods {
		chatId, err := method(username)
		if err == nil {
			color.Green("✅ Method %d ile %s chat ID'si alındı", i+1, username)
			return chatId, nil
		}
		color.Yellow("⚠️ Method %d başarısız: %v", i+1, err)
	}

	return 0, fmt.Errorf("tüm yöntemler başarısız")
}

func getChatIdFromWebPage(username string) (int, error) {
	// User-Agent ekleyerek normal tarayıcı gibi davran
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	url := fmt.Sprintf("https://kick.com/%s", username)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}

	// Browser headers ekle
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("sayfa yüklenemedi: %d", resp.StatusCode)
	}

	// HTML'den chat ID'yi regex ile bul
	body, err := readResponseBody(resp)
	if err != nil {
		return 0, err
	}

	// Chatroom ID'yi HTML'den bul
	chatRoomRegex := regexp.MustCompile(`"chatroom":\s*{\s*"id":\s*(\d+)`)
	matches := chatRoomRegex.FindStringSubmatch(string(body))
	if len(matches) < 2 {
		return 0, fmt.Errorf("HTML'de chat ID bulunamadı")
	}

	chatId, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, err
	}

	return chatId, nil
}

func getChatIdFromAPIv2(username string) (int, error) {
	// Farklı API endpoint'i dene
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	url := fmt.Sprintf("https://kick.com/api/v2/channels/%s", username)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", "https://kick.com/")

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("API v2 erişilemedi: %d", resp.StatusCode)
	}

	var channelData struct {
		Chatroom struct {
			ID int `json:"id"`
		} `json:"chatroom"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&channelData); err != nil {
		return 0, err
	}

	return channelData.Chatroom.ID, nil
}
func getChatIdFromRapidAPI(username string) (int, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	url := fmt.Sprintf("https://kick-com-api.p.rapidapi.com/channels/%s/chatroom", username)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("istek oluşturulamadı: %w", err)
	}

	req.Header.Add("x-rapidapi-key", "") // KENDİ key'in
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
func getKnownChatId(username string) int {
	// Bilinen chat ID'leri (güncelleyebilirsiniz)
	knownChatIds := map[string]int{
		//"buraksakinol": 25461130,
		// Buraya bilinen diğer chat ID'leri ekleyebilirsiniz
	}

	if chatId, exists := knownChatIds[username]; exists {
		return chatId
	}

	return 0
}

func askForManualChatId(username string) int {
	reader := bufio.NewReader(os.Stdin)

	color.Yellow("🔧 %s için chat ID'sini biliyorsanız girebilirsiniz", username)
	color.White("Chat ID'sini nasıl bulacağınız:")
	color.White("1. Tarayıcınızda https://kick.com/%s adresine gidin", username)
	color.White("2. F12 tuşuna basın -> Network sekmesine gidin")
	color.White("3. Sayfayı yenileyin ve 'chatrooms' içeren istekleri arayın")
	color.White("4. URL'de chatrooms.XXXXX.v2 formatında XXXXX sayısı chat ID'dir")
	color.White("")
	color.White("Chat ID'yi girin (boş bırakmak için Enter): ")

	idStr, _ := reader.ReadString('\n')
	idStr = strings.TrimSpace(idStr)

	if idStr == "" {
		return 0
	}

	chatId, err := strconv.Atoi(idStr)
	if err != nil {
		color.Red("❌ Geçersiz ID: %s", idStr)
		return 0
	}

	return chatId
}

func getChatIdFromAlternativeAPI(username string) (int, error) {
	// Bu metod artık gerekli değil, askForManualChatId kullanılıyor
	return 0, fmt.Errorf("alternatif API metodu kaldırıldı")
}

// HTTP response body okuma helper fonksiyonu
func readResponseBody(resp *http.Response) ([]byte, error) {
	var reader io.Reader = resp.Body

	// Gzip encoding kontrolü
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer gr.Close()
		reader = gr
	}

	return io.ReadAll(reader)
}

func WriteToDb(dataList []Data) {
	defer timer("WriteToDb")()
	if len(dataList) == 0 {
		return
	}

	valueStrings := make([]string, 0, len(dataList))
	valueArgs := make([]interface{}, 0, len(dataList)*4)

	for _, data := range dataList {
		valueStrings = append(valueStrings, "(?, ?, ?, ?)")
		valueArgs = append(valueArgs, data.Content, data.Sender.Username, ChatIdAndChannelMap[data.ChatroomID], data.CreatedAt)
	}

	color.Green("📝 BATCH WRITE: %d mesaj veritabanına yazıldı (simülasyon)", len(dataList))
}

func HandleDataChannelInserts(dataChannel <-chan Data) {
	dataBatch := make([]Data, 0, batchSize)
	start := time.Now()
	for data := range dataChannel {
		dataBatch = append(dataBatch, data)
		if len(dataBatch) >= batchSize {
			color.White("📦 Batch %v sürede doldu", time.Since(start))
			start = time.Now()
			WriteToDb(dataBatch)
			dataBatch = make([]Data, 0, batchSize)
		}
	}
}

func startListeningChat(streamerName string, chatRoomId int, dataChannel chan<- Data) {
	req, err := http.NewRequest(http.MethodGet, webSocketUrl, nil)
	if err != nil {
		log.Fatalf("request oluşturma hatası: %v", err)
	}

	conn, _, _, err := ws.Dialer{}.Dial(req.Context(), webSocketUrl)
	if err != nil {
		log.Printf("❌ %s (ID: %d) için websocket bağlantı hatası: %v", streamerName, chatRoomId, err)
		time.Sleep(5 * time.Second)
		go startListeningChat(streamerName, chatRoomId, dataChannel)
		return
	}
	defer conn.Close()

	message := []byte(fmt.Sprintf(chatroomSubcribeCommand, chatRoomId))
	err = wsutil.WriteClientMessage(conn, ws.OpText, message)
	if err != nil {
		log.Printf("❌ %s (ID: %d) için subscribe mesajı gönderme hatası: %v", streamerName, chatRoomId, err)
		conn.Close()
		time.Sleep(5 * time.Second)
		go startListeningChat(streamerName, chatRoomId, dataChannel)
		return
	}

	c := GetRandomColorForLog()
	color.Green("🎉 %s (ID: %d) başarıyla bağlandı ve dinleniyor", streamerName, chatRoomId)

	for {
		msg, _, err := wsutil.ReadServerData(conn)
		if err != nil {
			log.Printf("❌ %s (ID: %d) için mesaj okuma hatası: %v", streamerName, chatRoomId, err)
			time.Sleep(1 * time.Minute)
			go startListeningChat(streamerName, chatRoomId, dataChannel)
			return
		}

		go UnmarshallAndSendToChannel(streamerName, msg, dataChannel, c)
	}
}

func UnmarshallAndSendToChannel(streamerName string, msgByte []byte, dataChannel chan<- Data, c *color.Color) {
	var event Message
	if err := json.Unmarshal(msgByte, &event); err != nil {
		return
	}

	if event.Event != "App\\Events\\ChatMessageEvent" {
		return
	}

	var rawDataString string
	if err := json.Unmarshal(event.Data, &rawDataString); err != nil {
		return
	}

	var data Data
	if err := json.Unmarshal([]byte(rawDataString), &data); err != nil {
		return
	}

	if data.Type != "message" {
		return
	}

	dataChannel <- data

	// Link kontrolü ve özel gösterim
	if linkRegex.MatchString(data.Content) {
		links := linkRegex.FindAllString(data.Content, -1)

		// Normal mesajı göster
		c.Printf("💬 %s:%s:%s\n", streamerName, data.Sender.Username, data.Content)

		// Linkleri özel olarak göster
		for _, link := range links {
			color.New(color.FgHiYellow, color.Bold).Printf("🔗 [%s] %s LINK: %s\n",
				streamerName, data.Sender.Username, link)
		}
	} else {
		// Normal mesaj gösterimi
		c.Printf("💬 %s:%s:%s\n", streamerName, data.Sender.Username, data.Content)
	}
}

func GetRandomColorForLog() *color.Color {
	colors := []*color.Color{
		color.New(color.FgRed),
		color.New(color.FgGreen),
		color.New(color.FgYellow),
		color.New(color.FgCyan),
		color.New(color.FgBlue),
		color.New(color.FgMagenta),
	}
	return colors[rand.Intn(len(colors))]
}

func timer(name string) func() {
	start := time.Now()
	return func() {
		color.White("⏱️ %s %v sürede tamamlandı", name, time.Since(start))
	}
}
