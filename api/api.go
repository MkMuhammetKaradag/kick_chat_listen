package api

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"kick-chat/config"
	"net/http"
	"time"
)

func GetChatIdFromKick(username string) (int, error) {
	methods := []func(string) (int, error){
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
