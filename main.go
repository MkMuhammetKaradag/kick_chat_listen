package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/fatih/color"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

const batchSize = 10
const webSocketUrl string = "wss://ws-us2.pusher.com/app/32cbd69e4b950bf97679?protocol=7&client=js&version=8.4.0&flash=false"
const chatroomSubcribeCommand string = "{\"event\":\"pusher:subscribe\",\"data\":{\"auth\":\"\",\"channel\":\"chatrooms.%d.v2\"}}"

// Global maps (bunları dosyanızda uygun bir yere tanımlamalısınız)

func main() {

	rand.New(rand.NewSource(time.Now().UnixNano()))
	dataChannel := make(chan Data, 100)
	go HandleDataChannelInserts(dataChannel)

	for channelName, chatRoomId := range ChannelAndChatIdMap {
		go startListeningChat(channelName, chatRoomId, dataChannel)
	}

	// Block main goroutine
	select {}
}

func WriteToDb(dataList []Data) {
	defer timer("WriteToDb")()
	if len(dataList) == 0 {
		return
	}

	valueStrings := make([]string, 0, len(dataList))
	valueArgs := make([]interface{}, 0, len(dataList)*4) // Each data has 4 fields

	// Construct the query string with placeholders for each data
	for _, data := range dataList {
		valueStrings = append(valueStrings, "(?, ?, ?, ?)")
		valueArgs = append(valueArgs, data.Content, data.Sender.Username, ChatIdAndChannelMap[data.ChatroomID], data.CreatedAt)
	}

	fmt.Println(valueStrings)
	color.Green("BATCH WRITE: %d messages to DB (simulated)", len(dataList)) // Simülasyon
}

func HandleDataChannelInserts(dataChannel <-chan Data) {
	dataBatch := make([]Data, 0, batchSize)
	start := time.Now()
	for data := range dataChannel {
		dataBatch = append(dataBatch, data)
		if len(dataBatch) >= batchSize {
			color.White("-> Batch filled at %v\n", time.Since(start))
			start = time.Now()
			WriteToDb(dataBatch)
			dataBatch = make([]Data, 0, batchSize)
		}
	}
}

func startListeningChat(streamerName string, chatRoomId int, dataChannel chan<- Data) {
	req, err := http.NewRequest(http.MethodGet, webSocketUrl, nil)
	if err != nil {
		log.Fatalf("error creating request: %v", err)
	}

	// Connect to the server.
	conn, _, _, err := ws.Dialer{}.Dial(req.Context(), webSocketUrl)
	if err != nil {
		log.Printf("error connecting to websocket for %s (ID: %d): %v", streamerName, chatRoomId, err)
		// Hata durumunda tekrar deneme mekanizması ekleyebilirsiniz
		time.Sleep(5 * time.Second)                                  // Kısa bir bekleme
		go startListeningChat(streamerName, chatRoomId, dataChannel) // Yeniden dene
		return
	}
	defer conn.Close()

	// Send a message to the WebSocket server to subscribe to chatroom.
	message := []byte(fmt.Sprintf(chatroomSubcribeCommand, chatRoomId))
	err = wsutil.WriteClientMessage(conn, ws.OpText, message)
	if err != nil {
		log.Printf("error sending subscribe message for %s (ID: %d): %v", streamerName, chatRoomId, err)
		conn.Close() // Bağlantıyı kapat ve tekrar dene
		time.Sleep(5 * time.Second)
		go startListeningChat(streamerName, chatRoomId, dataChannel)
		return
	}

	c := GetRandomColorForLog()
	color.Green("Successfully connected and subscribed to %s (ID: %d)", streamerName, chatRoomId)

	for {
		msg, _, err := wsutil.ReadServerData(conn)
		if err != nil {
			log.Printf("error reading message for %s (ID: %d): %v", streamerName, chatRoomId, err)
			// Bağlantı koparsa veya hata olursa yeniden bağlanmayı dene
			time.Sleep(1 * time.Minute) // Daha uzun bir bekleme
			go startListeningChat(streamerName, chatRoomId, dataChannel)
			return // Mevcut goroutine'i sonlandır
		}

		go UnmarshallAndSendToChannel(streamerName, msg, dataChannel, c)
	}
}
func UnmarshallAndSendToChannel(streamerName string, msgByte []byte, dataChannel chan<- Data, c *color.Color) {
	var event Message
	if err := json.Unmarshal(msgByte, &event); err != nil {
		// Hata ayıklama için bu logu açık bırakmak isteyebilirsiniz
		fmt.Println("Error unmarshaling main event struct:", err)
		return
	}

	// Sadece "App\Events\ChatMessageEvent" türündeki olayları işle
	// Not: Pusher'ın belgelerinde bu event ismi "pusher:pong" veya "pusher:subscribe" gibi farklı olabilir.
	// Kick'in tam event ismini kontrol etmek için gerçek loglara bakın.
	// Eğer "App\\Events\\ChatMessageEvent" yerine "App\\ChatMessageEvent" ise önceki kodunuz doğruydu.
	if event.Event != "App\\Events\\ChatMessageEvent" {
		// fmt.Printf("Skipping event type: %s\n", event.Event) // Hata ayıklama için
		return
	}

	// İlk adım: json.RawMessage'i bir string olarak çözümle
	var rawDataString string
	if err := json.Unmarshal(event.Data, &rawDataString); err != nil {
		// Eğer data alanı doğrudan bir JSON objesi ise bu hata tetiklenir.
		// Bu durumda ikinci bir deneme yapabiliriz veya varsayılan olarak bunun bir string olduğunu varsayabiliriz.
		// Şimdilik sadece hatayı yazdıralım.
		fmt.Println("Error unmarshaling event.Data to string:", err)
		return
	}

	// İkinci adım: String olarak alınan JSON verisini (rawDataString) []byte'a çevirip Data struct'ına çözümle
	var data Data
	if err := json.Unmarshal([]byte(rawDataString), &data); err != nil {
		fmt.Println("Error unmarshaling final Data struct from raw string:", err)
		return
	}

	// Buradaki data.Type kontrolünü de tekrar gözden geçirin.
	// Eğer gelen tüm mesajlar "message" tipinde ise bu kontrol gerekli olmayabilir.
	// Ama farklı tiplerde mesajlar geliyorsa (örneğin "follow", "subscribe" vb. gibi) faydalıdır.
	if data.Type != "message" {
		return
	}

	dataChannel <- data

	c.Printf("%s:%s:%s \n", streamerName, data.Sender.Username, data.Content)
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

// timer returns a function that prints the name argument and
// the elapsed time between the call to timer and the call to
// the returned function. The returned function is intended to
// be used in a defer statement:
//
//	defer timer("sum")()
func timer(name string) func() {
	start := time.Now()
	return func() {
		color.White("-> %s took %v\n", name, time.Since(start))
	}
}
