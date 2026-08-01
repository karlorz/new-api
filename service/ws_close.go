package service

import "github.com/QuantumNous/new-api/pkg/wsmanager"

const ChannelDisabledCloseReason = wsmanager.DefaultCloseReason

func CloseActiveWebSocketsForChannel(channelID int, reason string) int {
	return wsmanager.CloseChannelsAndBroadcast([]int{channelID}, reason)
}

func CloseActiveWebSocketsForChannels(channelIDs []int, reason string) int {
	return wsmanager.CloseChannelsAndBroadcast(channelIDs, reason)
}
