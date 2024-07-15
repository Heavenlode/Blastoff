package blastoff

import (
	"log"
	"sync"

	"github.com/codecat/go-enet"
	"github.com/google/uuid"
)

func CreateServer(address enet.Address) *BlastoffServer {
	return &BlastoffServer{
		Address:          address,
		remoteAddressMap: make(map[uuid.UUID]enet.Address),
	}
}

func NewAddress(ip string, port uint16) enet.Address {
	return enet.NewAddress(ip, port)
}

func (server *BlastoffServer) AddRemote(uuid uuid.UUID, address enet.Address) {
	server.remoteAddressMap[uuid] = address
}

func (server *BlastoffServer) Start() {
	enet.Initialize()
	host, err := enet.NewHost(server.Address, 1024, 32, 0, 0)
	if err != nil {
		log.Fatalf("Couldn't create host: %s\n", err.Error())
		return
	}

	log.Printf("Server started %s:%d\n", server.Address.String(), server.Address.GetPort())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {

		// The main Blastoff server loop
		// These peers are attempting to connect to remotes
		for {
			ev := host.Service(10)

			if ev.GetType() == enet.EventNone {
				continue
			}

			switch ev.GetType() {
			case enet.EventConnect:
				log.Printf("New peer connected: %s\n", ev.GetPeer().GetAddress())
				server.peerMap[ev.GetPeer()] = peerData{
					Peer:          ev.GetPeer(),
					PacketChannel: make(chan bridgePacket, 10),
					CloseSignal:   make(chan bool, 10),
				}
				go server.bridgePeerToRemote(ev.GetPeer(), server.peerMap[ev.GetPeer()].PacketChannel, server.peerMap[ev.GetPeer()].CloseSignal)

			case enet.EventDisconnect:
				log.Printf("Peer disconnected: %s\n", ev.GetPeer().GetAddress())
				server.peerMap[ev.GetPeer()].CloseSignal <- true
				delete(server.peerMap, ev.GetPeer())

			case enet.EventReceive:
				if ev.GetChannelID() == RemoteAdminChannelId {
					// Discard messages on this channel, because this is for remotes to communicate with Blastoff
					// If we were to forward this message to a remote, it would be misinterpreted as a message from Blastoff
					// Rather than a bridged client message
					ev.GetPacket().Destroy()
					log.Printf("Warning: discarded illegal client message")
				} else {
					packet := ev.GetPacket()
					// Forward all other client messages to the remote
					server.peerMap[ev.GetPeer()].PacketChannel <- bridgePacket{packet, ev.GetChannelID()}
				}
			}
		}
		wg.Done()
	}()

	wg.Wait()

	// Destroy the host when we're done with it
	host.Destroy()

	// Uninitialize enet
	enet.Deinitialize()
}
