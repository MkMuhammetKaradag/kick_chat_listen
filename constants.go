package main

var ChannelAndChatIdMap = map[string]int{
	"buraksakinol": 25461130,
}

var ChatIdAndChannelMap = map[int]string{
	25461130: "buraksakinol",
}

func init() {
	for k, v := range ChannelAndChatIdMap {
		ChatIdAndChannelMap[v] = k
	}
}
