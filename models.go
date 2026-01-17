package blastoff

import (
	"github.com/Heavenlode/Blastoff/internal/enet"
	"github.com/google/uuid"
)

type bridgePacket struct {
	packet    *enet.Packet
	channelID uint8
}

// PeerKey is used as a map key for peers
type PeerKey uintptr

type peerData struct {
	Peer          *enet.Peer
	PacketChannel chan bridgePacket
	CloseSignal   chan bool
}

type BlastoffServer struct {
	Address          enet.Address
	remoteAddressMap map[uuid.UUID]enet.Address
	peerMap          map[PeerKey]peerData
}

// This channel is used for the Remote to communicate with the Blastoff server
const RemoteAdminChannelId uint8 = 249

// Commands which the Remote can send to the Blastoff server
type ServerCommandFlag uint8

const (
	// Request Blastoff to instantiate a new remote instance
	ServerCommandNewInstance ServerCommandFlag = iota

	// Confirm Blastoff to bridge connection
	ServerCommandValidateClient

	// Request Blastoff to redirect client to another remote
	ServerCommandRedirectClient
)
