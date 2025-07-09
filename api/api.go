package api

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"kick-chat/config"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

func GetChatIdFromKick(username string) (int, error) {
	methods := []func(string) (int, error){
		getChatIdFromWebPage,
		getChatIdFromAPIv2,
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

func getChatIdFromWebPage(username string) (int, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("https://kick.com/%s", username)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("sayfa yüklenemedi: %d", resp.StatusCode)
	}
	body, err := readResponseBody(resp)
	if err != nil {
		return 0, err
	}
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
	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("https://kick.com/api/v2/channels/%s", username)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "application/json")
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
	fmt.Println("rapid_api:", config.GetRapidAPIKey())

	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("https://kick-com-api.p.rapidapi.com/channels/%s/chatroom", username)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("istek oluşturulamadı: %w", err)
	}
	req.Header.Add("x-rapidapi-key", config.GetRapidAPIKey())
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

func readResponseBody(resp *http.Response) ([]byte, error) {
	var reader io.Reader = resp.Body
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
