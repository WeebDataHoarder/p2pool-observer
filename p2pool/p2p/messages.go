package p2p

type MessageId uint8

// from p2p_server.h
const (
	MessageHandshakeChallenge MessageId = iota
	MessageHandshakeSolution
	MessageListenPort
	MessageBlockRequest
	MessageBlockResponse
	MessageBlockBroadcast
	MessagePeerListRequest
	MessagePeerListResponse
	// MessageBlockBroadcastCompact Protocol 1.1
	MessageBlockBroadcastCompact
)