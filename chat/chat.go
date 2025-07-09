package chat

import (
	"bufio"
	"encoding/json"
	"fmt"

	"kick-chat/api"
	"kick-chat/config"
	"kick-chat/models"
	"kick-chat/utils"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/logrusorgru/aurora"
)

var linkRegex = regexp.MustCompile(`https?://[^\s]+`)

func GetUsernames() []string {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println(aurora.Yellow("🎯 Kick Chat Dinleyici"))
	fmt.Println(aurora.White("Dinlemek istediğiniz kullanıcı adlarını girin:"))
	fmt.Println(aurora.White("- Tek kullanıcı: username"))
	fmt.Println(aurora.White("- Çoklu kullanıcı: username1,username2,username3"))
	fmt.Println(aurora.White("- Veya her satırda bir kullanıcı adı yazabilirsiniz (boş satır ile bitirin)"))
	fmt.Print("\n> ")

	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	var usernames []string
	if strings.Contains(input, ",") {
		usernames = strings.Split(input, ",")
		for i, username := range usernames {
			usernames[i] = strings.TrimSpace(username)
		}
	} else if input != "" {
		usernames = append(usernames, input)
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

	var filteredUsernames []string
	for _, username := range usernames {
		if username != "" {
			filteredUsernames = append(filteredUsernames, username)
		}
	}

	return filteredUsernames
}

func FetchChatIds(usernames []string) error {
	fmt.Println(aurora.Yellow("🔍 Chat ID'leri alınıyor..."))
	fmt.Println(aurora.Yellow("⚠️ Kick.com Cloudflare koruması kullanıyor - manuel ID girişi önerilir"))

	for _, username := range usernames {
		// api.GetChatIdFromKick fonksiyonu tanımlanmadığı için yorum satırı yaptım
		// Gerçek implementasyonu ekleyebilirsiniz

		chatId, err := api.GetChatIdFromKick(username)
		if err != nil {
			fmt.Println(aurora.Red(fmt.Sprintf("❌ %s için chat ID alınamadı: %v", username, err)))
			if knownId := getKnownChatId(username); knownId != 0 {
				fmt.Println(aurora.Green(fmt.Sprintf("✅ %s için bilinen chat ID kullanılıyor: %d", username, knownId)))
				config.ChannelAndChatIdMap[username] = knownId
				config.ChatIdAndChannelMap[knownId] = username
				continue
			}
			if manualId := askForManualChatId(username); manualId != 0 {
				config.ChannelAndChatIdMap[username] = manualId
				config.ChatIdAndChannelMap[manualId] = username
				fmt.Println(aurora.Green(fmt.Sprintf("✅ %s -> %d (manuel)", username, manualId)))
				continue
			}
			fmt.Println(aurora.Red(fmt.Sprintf("❌ %s atlanıyor", username)))
			continue
		}
		config.ChannelAndChatIdMap[username] = chatId
		config.ChatIdAndChannelMap[chatId] = username
		fmt.Println(aurora.Green(fmt.Sprintf("✅ %s -> %d", username, chatId)))

		// // Geçici olarak bilinen ID'leri kontrol et
		// if knownId := getKnownChatId(username); knownId != 0 {
		// 	fmt.Println(aurora.Green(fmt.Sprintf("✅ %s için bilinen chat ID kullanılıyor: %d", username, knownId)))
		// 	config.ChannelAndChatIdMap[username] = knownId
		// 	config.ChatIdAndChannelMap[knownId] = username
		// 	continue
		// }
		// if manualId := askForManualChatId(username); manualId != 0 {
		// 	config.ChannelAndChatIdMap[username] = manualId
		// 	config.ChatIdAndChannelMap[manualId] = username
		// 	fmt.Println(aurora.Green(fmt.Sprintf("✅ %s -> %d (manuel)", username, manualId)))
		// 	continue
		// }
		// fmt.Println(aurora.Red(fmt.Sprintf("❌ %s atlanıyor", username)))
	}

	if len(config.ChannelAndChatIdMap) == 0 {
		return fmt.Errorf("hiçbir kullanıcı için chat ID alınamadı")
	}

	return nil
}

func DisplayConnectedChannels() {
	fmt.Println(aurora.Green("✅ Dinlenecek kanallar:"))
	for username, chatId := range config.ChannelAndChatIdMap {
		fmt.Println(aurora.Cyan(fmt.Sprintf("   • %s (ID: %d)", username, chatId)))
	}
	fmt.Println()
}

func HandleDataChannelInserts(dataChannel <-chan config.Data) {
	dataBatch := make([]config.Data, 0, config.BatchSize) // config.batchSize -> config.BatchSize
	start := time.Now()
	for data := range dataChannel {
		dataBatch = append(dataBatch, data)
		if len(dataBatch) >= config.BatchSize {
			fmt.Println(aurora.White(fmt.Sprintf("📦 Batch %v sürede doldu", time.Since(start))))
			start = time.Now()
			WriteToDb(dataBatch)
			dataBatch = make([]config.Data, 0, config.BatchSize)
		}
	}
}

func WriteToDb(dataList []config.Data) {
	defer utils.Timer("WriteToDb")()
	if len(dataList) == 0 {
		return
	}

	fmt.Println(aurora.Green(fmt.Sprintf("📝 BATCH WRITE: %d mesaj veritabanına yazıldı (simülasyon)", len(dataList))))
}

func StartListeningChat(streamerName string, chatRoomId int, dataChannel chan<- config.Data) {
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(config.WebSocketUrl, nil) // config.webSocketUrl -> config.WebSocketUrl
	if err != nil {
		log.Printf("❌ %s (ID: %d) için websocket bağlantı hatası: %v", streamerName, chatRoomId, err)
		time.Sleep(5 * time.Second)
		go StartListeningChat(streamerName, chatRoomId, dataChannel)
		return
	}
	defer conn.Close()

	message := []byte(fmt.Sprintf(config.ChatroomSubscribeCommand, chatRoomId)) // config.chatroomSubcribeCommand -> config.ChatroomSubscribeCommand
	err = conn.WriteMessage(websocket.TextMessage, message)
	if err != nil {
		log.Printf("❌ %s (ID: %d) için subscribe mesajı gönderme hatası: %v", streamerName, chatRoomId, err)
		conn.Close()
		time.Sleep(5 * time.Second)
		go StartListeningChat(streamerName, chatRoomId, dataChannel)
		return
	}

	c := utils.GetRandomColorForLog()
	fmt.Println(aurora.Green(fmt.Sprintf("🎉 %s (ID: %d) başarıyla bağlandı ve dinleniyor", streamerName, chatRoomId)))

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("❌ %s (ID: %d) için mesaj okuma hatası: %v", streamerName, chatRoomId, err)
			time.Sleep(1 * time.Minute)
			go StartListeningChat(streamerName, chatRoomId, dataChannel)
			return
		}

		go UnmarshallAndSendToChannel(streamerName, msg, dataChannel, c)
	}
}

func UnmarshallAndSendToChannel(streamerName string, msgByte []byte, dataChannel chan<- config.Data, c aurora.Color) {
	var event models.Message
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

	var data config.Data
	if err := json.Unmarshal([]byte(rawDataString), &data); err != nil {
		return
	}

	if data.Type != "message" {
		return
	}

	dataChannel <- data

	if linkRegex.MatchString(data.Content) {
		links := linkRegex.FindAllString(data.Content, -1)
		fmt.Print(aurora.Colorize(fmt.Sprintf("💬 %s:%s:%s\n", streamerName, data.Sender.Username, data.Content), c))
		for _, link := range links {
			fmt.Print(aurora.Colorize(fmt.Sprintf("🔗 [%s] %s LINK: %s\n", streamerName, data.Sender.Username, link), aurora.YellowFg|aurora.BoldFm))
		}
	} else {
		fmt.Print(aurora.Colorize(fmt.Sprintf("💬 %s:%s:%s\n", streamerName, data.Sender.Username, data.Content), c))
	}
}

func getKnownChatId(username string) int {
	knownChatIds := map[string]int{
		// "buraksakinol": 25461130,
		// Buraya bilinen diğer chat ID'leri ekleyebilirsiniz
	}
	if chatId, exists := knownChatIds[username]; exists {
		return chatId
	}
	return 0
}

func askForManualChatId(username string) int {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println(aurora.Yellow(fmt.Sprintf("🔧 %s için chat ID'sini biliyorsanız girebilirsiniz", username)))
	fmt.Println(aurora.White("Chat ID'sini nasıl bulacağınız:"))
	fmt.Println(aurora.White(fmt.Sprintf("1. Tarayıcınızda https://kick.com/%s adresine gidin", username)))
	fmt.Println(aurora.White("2. F12 tuşuna basın -> Network sekmesine gidin"))
	fmt.Println(aurora.White("3. Sayfayı yenileyin ve 'chatrooms' içeren istekleri arayın"))
	fmt.Println(aurora.White("4. URL'de chatrooms.XXXXX.v2 formatında XXXXX sayısı chat ID'dir"))
	fmt.Println(aurora.White(""))
	fmt.Print(aurora.White("Chat ID'yi girin (boş bırakmak için Enter): "))
	idStr, _ := reader.ReadString('\n')
	idStr = strings.TrimSpace(idStr)
	if idStr == "" {
		return 0
	}
	chatId, err := strconv.Atoi(idStr)
	if err != nil {
		fmt.Println(aurora.Red(fmt.Sprintf("❌ Geçersiz ID: %s", idStr)))
		return 0
	}
	return chatId
}
