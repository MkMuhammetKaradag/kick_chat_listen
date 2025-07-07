package main

import "encoding/json"

type Message struct {
	Event   string          `json:"event"`
	Data    json.RawMessage `json:"data"`
	Channel string          `json:"channel"`
}

type Data struct {
	Content    string `json:"content"`
	Sender     Sender `json:"sender"`
	ChatroomID int    `json:"chatroom_id"`
	Type       string `json:"type"`
	CreatedAt  string `json:"created_at"`
}

type Sender struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Slug     string `json:"slug"`
}

type Identity struct {
	Color  string  `json:"color"`
	Badges []Badge `json:"badges"`
}

type Badge struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
