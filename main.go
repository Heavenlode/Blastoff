package blastoff

import (
	"log"
	"sync"

	"github.com/codecat/go-enet"
	"github.com/google/uuid"
)

type bridgePacket struct {
	packet    enet.Packet
	channelID uint8
}

type ServerCommandFlag uint8

const (
	ServerCommandNewInstance ServerCommandFlag = iota
	ServerCommandValidateClient
	ServerCommandRedirectClient
)

type BlastoffServer struct {
	Address          enet.Address
	remoteAddressMap map[uuid.UUID]enet.Address
}

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

func (server *BlastoffServer) bridgePeerToRemote(peer enet.Peer, packetChan <-chan bridgePacket, closeSignal <-chan bool) {
	remoteHost, err := enet.NewHost(nil, 32, 256, 0, 0)
	if err != nil {
		log.Fatalf("Couldn't create host: %s", err.Error())
		return
	}
	var closed = false
	var open = false
	var peerToken []byte

	var redirectChan = make(chan uuid.UUID, 1)

	go func() {
		// Connect the client host to the server
		var remote enet.Peer
	root:
		for {
			select {
			case uuid := <-redirectChan:
				// The client is being redirected to another server
				remote.Disconnect(0)
				remote, err = remoteHost.Connect(server.remoteAddressMap[uuid], 256, 0)
				if err != nil {
					log.Printf("Couldn't connect to server: %s\n", err.Error())
					break root
				}
				err = remote.SendBytes(peerToken, 255, enet.PacketFlagReliable)
				if err != nil {
					log.Printf("Couldn't send token to server: %s\n", err.Error())
					break root
				}
			case _bridgePacket := <-packetChan:
				if !open {
					// Initialize the connection with the remote host.
					// First, the client sends a packet:
					// The first part is the UUID of the remote host they wish to connect to
					// The second part is the token that the remote host will use to verify the connection
					var data = _bridgePacket.packet.GetData()
					_bridgePacket.packet.Destroy()
					if len(data) <= 36 {
						log.Println("Invalid packet data length")
						break root
					}
					uuid, err := uuid.FromBytes(data[:36])
					if err != nil {
						log.Printf("Couldn't parse UUID: %s\n", err.Error())
						break root
					}
					if _, ok := server.remoteAddressMap[uuid]; !ok {
						log.Printf("Remote host ID %s not found.", uuid.String())
						break root
					}
					peerToken = data[36:]
					// Now we connect to the remote
					remote, err = remoteHost.Connect(server.remoteAddressMap[uuid], 256, 0)
					if err != nil {
						log.Printf("Couldn't connect to server: %s\n", err.Error())
						break root
					}
					err = remote.SendBytes(peerToken, 255, enet.PacketFlagReliable)
					if err != nil {
						log.Printf("Couldn't send token to server: %s\n", err.Error())
						break root
					}
					continue
				}
				if err := remote.SendPacket(_bridgePacket.packet, _bridgePacket.channelID); err != nil {
					log.Printf("Couldn't send packet to server: %s\n", err.Error())
					_bridgePacket.packet.Destroy()
				}
			case <-closeSignal:
				break root
			}
		}
		closed = true
	}()

	for {
		if closed {
			break
		}
		ev := remoteHost.Service(5000)
		if ev.GetType() == enet.EventNone {
			log.Println("Bridge timed out")
			peer.Disconnect(0)
			break
		}

		switch ev.GetType() {
		case enet.EventDisconnect:
			peer.Disconnect(0)

		case enet.EventReceive:
			packet := ev.GetPacket()
			if ev.GetChannelID() == 255 {
				var data = packet.GetData()
				packet.Destroy()
				// This is a special communication packet from the server
				// The first byte indicates the type of packet
				switch ServerCommandFlag(data[0]) {
				case ServerCommandNewInstance:
					// The server is redirecting the client to another server
					// The data contains the UUID of the server to redirect to
					if len(data) < 37 {
						log.Println("Invalid new instance data")
						peer.Disconnect(0)
						break
					}
					uuid, err := uuid.FromBytes(data[1:37])
					if err != nil {
						log.Printf("Couldn't parse UUID: %s\n", err.Error())
						peer.Disconnect(0)
						break
					}
					log.Printf("Spawning new instance: %s\n", uuid.String())
				case ServerCommandValidateClient:
					open = true
				case ServerCommandRedirectClient:
					// The client is being redirected to another server
					// The data contains the UUID of the server to redirect to
					if len(data) < 37 {
						log.Println("Invalid redirect data length")
						peer.Disconnect(0)
						break
					}
					uuid, err := uuid.FromBytes(data[1:37])
					if err != nil {
						log.Printf("Couldn't parse UUID: %s\n", err.Error())
						peer.Disconnect(0)
						break
					}
					if _, ok := server.remoteAddressMap[uuid]; !ok {
						log.Printf("Remote host ID %s not found during redirect.", uuid.String())
						peer.Disconnect(0)
						break
					}
				default:
					log.Println("Client validation failed")
					peer.Disconnect(0)
					break
				}
				continue
			} else {
				if !open {
					// This is a bug. The server should not be sending messages through this stream before the client is validated.
					log.Println("Client not validated.")
					peer.Disconnect(0)
					break
				}
				if err := peer.SendPacket(packet, ev.GetChannelID()); err != nil {
					log.Printf("Couldn't send packet to client: %s\n", err.Error())
					packet.Destroy()
				}
			}
		}
	}
	remoteHost.Destroy()
}

func (server *BlastoffServer) Start() {
	enet.Initialize()
	host, err := enet.NewHost(enet.NewListenAddress(20406), 1024, 32, 0, 0)
	if err != nil {
		log.Fatalf("Couldn't create host: %s\n", err.Error())
		return
	}

	log.Println("Server started on 20406")
	var packetChan = make(chan bridgePacket, 10)
	var closeSignal = make(chan bool, 10)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for {
			ev := host.Service(10)

			if ev.GetType() == enet.EventNone {
				continue
			}

			switch ev.GetType() {
			case enet.EventConnect:
				log.Printf("New peer connected: %s\n", ev.GetPeer().GetAddress())
				go server.bridgePeerToRemote(ev.GetPeer(), packetChan, closeSignal)

			case enet.EventDisconnect:
				log.Printf("Peer disconnected: %s\n", ev.GetPeer().GetAddress())
				closeSignal <- true

			case enet.EventReceive:
				packet := ev.GetPacket()
				packetChan <- bridgePacket{packet, ev.GetChannelID()}
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
