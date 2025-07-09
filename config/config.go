package config

import (
	"log"
	"os"
	"sync"
	"time"

	"github.com/joho/godotenv"
)

var (
	ChannelAndChatIdMap      = make(map[string]int)
	ChatIdAndChannelMap      = make(map[int]string)
	WebSocketUrl             = "wss://ws-us2.pusher.com/app/32cbd69e4b950bf97679?protocol=7&client=js&version=8.4.0&flash=false"
	ChatroomSubscribeCommand = "{\"event\":\"pusher:subscribe\",\"data\":{\"auth\":\"\",\"channel\":\"chatrooms.%d.v2\"}}"
	BatchSize                = 10
)

var rapidAPIKey string
var once sync.Once

type Data struct {
	Type       string    `json:"type"`
	Content    string    `json:"content"`
	ChatroomID int       `json:"chatroom_id"`
	CreatedAt  time.Time `json:"created_at"`
	Sender     struct {
		Username string `json:"username"`
	} `json:"sender"`
}

func LoadEnv() {
	once.Do(func() {
		err := godotenv.Load()
		if err != nil {
			log.Fatal("Error loading .env file")
		}
		rapidAPIKey = os.Getenv("RAPIDAPI_KEY")
		if rapidAPIKey == "" {
			log.Fatal("RAPIDAPI_KEY not found in .env")
		}
	})
}

func GetRapidAPIKey() string {
	return rapidAPIKey
}
