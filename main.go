package blastoff

import (
	"log"
	"sync"

	"github.com/Heavenlode/Blastoff/internal/enet"
	"github.com/google/uuid"
)

func CreateServer(address enet.Address) *BlastoffServer {
	return &BlastoffServer{
		Address:          address,
		remoteAddressMap: make(map[uuid.UUID]enet.Address),
		peerMap:          make(map[PeerKey]peerData),
	}
}

func NewAddress(ip string, port uint16) enet.Address {
	addr, err := enet.NewAddress(ip, port)
	if err != nil {
		log.Fatalf("Failed to create address: %s", err.Error())
	}
	return addr
}

var defaultRemote enet.Address

func (server *BlastoffServer) AddRemote(uuid uuid.UUID, address enet.Address) {
	server.remoteAddressMap[uuid] = address
	if defaultRemote.GetPort() == 0 {
		defaultRemote = address
	}
}

func (server *BlastoffServer) Start() {
	if err := enet.Initialize(); err != nil {
		log.Fatalf("Failed to initialize ENet: %s", err.Error())
		return
	}
	host, err := enet.NewHost(&server.Address, 1024, 0, 0, 0)
	if err != nil {
		log.Fatalf("Couldn't create host: %s\n", err.Error())
		return
	}
	err = host.CompressWithRangeCoder()
	if err != nil {
		log.Fatalf("Couldn't enable compression mode: %s\n", err.Error())
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
				peer := ev.GetPeer()
				peerKey := PeerKey(peer.Pointer())
				log.Printf("New peer connected: %s\n", peer.GetAddress())
				server.peerMap[peerKey] = peerData{
					Peer:          peer,
					PacketChannel: make(chan bridgePacket, 10),
					CloseSignal:   make(chan bool, 10),
				}
				go server.bridgePeerToRemote(peer, server.peerMap[peerKey].PacketChannel, server.peerMap[peerKey].CloseSignal)

			case enet.EventDisconnect:
				peer := ev.GetPeer()
				peerKey := PeerKey(peer.Pointer())
				log.Printf("Peer disconnected: %s\n", peer.GetAddress())
				if data, ok := server.peerMap[peerKey]; ok {
					data.CloseSignal <- true
					delete(server.peerMap, peerKey)
				}

			case enet.EventReceive:
				peer := ev.GetPeer()
				peerKey := PeerKey(peer.Pointer())
				packet := ev.GetPacket()
				log.Printf("[Main] Received packet from %s on channel %d (%d bytes)\n", peer.GetAddress(), ev.GetChannelID(), packet.GetLength())
				// Forward all client messages to the remote
				if data, ok := server.peerMap[peerKey]; ok {
					data.PacketChannel <- bridgePacket{packet, ev.GetChannelID()}
				} else {
					log.Printf("[Main] WARNING: No peer data found for %s\n", peer.GetAddress())
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
