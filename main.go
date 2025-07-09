package main

import (
	"kick-chat/chat"
	"kick-chat/config"
)

func main() {
	config.LoadEnv()

	usernames := chat.GetUsernames()
	if len(usernames) == 0 {
		return
	}

	err := chat.FetchChatIds(usernames)
	if err != nil {
		return
	}

	chat.DisplayConnectedChannels()

	dataChannel := make(chan config.Data, 100)
	go chat.HandleDataChannelInserts(dataChannel)

	for channelName, chatRoomId := range config.ChannelAndChatIdMap {
		go chat.StartListeningChat(channelName, chatRoomId, dataChannel)
	}

	select {}
}
