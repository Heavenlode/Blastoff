package main

import (
	"log"
	"sync"

	"github.com/codecat/go-enet"
	"github.com/google/uuid"
)

var remoteAddressMap = map[uuid.UUID]enet.Address{
	uuid.MustParse("8a2773c9-b1c6-4f8f-a4f1-59d5dde00752"): enet.NewAddress("127.0.0.1", 8888),
}

type BridgePacket struct {
	packet    enet.Packet
	channelID uint8
}

// We define an enum of the different types of packets that can be sent
type ClientValidationFlag uint8

const (
	ClientValidationValid ClientValidationFlag = iota
	ClientValidationRedirect
	// There may be others in the future, for example:
	// ClientValidationFlagBanned
)

func bridgePeerToRemote(peer enet.Peer, packetChan <-chan BridgePacket, closeSignal <-chan bool) {
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
				remote, err = remoteHost.Connect(remoteAddressMap[uuid], 256, 0)
				if err != nil {
					log.Printf("Couldn't connect to server: %s\n", err.Error())
					break root
				}
				err = remote.SendBytes(peerToken, 255, enet.PacketFlagReliable)
				if err != nil {
					log.Printf("Couldn't send token to server: %s\n", err.Error())
					break root
				}
			case bridgePacket := <-packetChan:
				if !open {
					// Initialize the connection with the remote host.
					// First, the client sends a packet:
					// The first part is the UUID of the remote host they wish to connect to
					// The second part is the token that the remote host will use to verify the connection
					var data = bridgePacket.packet.GetData()
					bridgePacket.packet.Destroy()
					if len(data) <= 36 {
						log.Println("Invalid packet data length")
						break root
					}
					uuid, err := uuid.FromBytes(data[:36])
					if err != nil {
						log.Printf("Couldn't parse UUID: %s\n", err.Error())
						break root
					}
					if _, ok := remoteAddressMap[uuid]; !ok {
						log.Printf("Remote host ID %s not found.", uuid.String())
						break root
					}
					peerToken = data[36:]
					// Now we connect to the remote
					remote, err = remoteHost.Connect(remoteAddressMap[uuid], 256, 0)
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
				if err := remote.SendPacket(bridgePacket.packet, bridgePacket.channelID); err != nil {
					log.Printf("Couldn't send packet to server: %s\n", err.Error())
					bridgePacket.packet.Destroy()
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
			if !open {
				// This is the first packet from the remote host
				// This indicates whether the client has a valid connection or not.
				var data = packet.GetData()
				packet.Destroy()
				switch ClientValidationFlag(data[0]) {
				case ClientValidationValid:
					open = true
				case ClientValidationRedirect:
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
					if _, ok := remoteAddressMap[uuid]; !ok {
						log.Printf("Remote host ID %s not found during redirect.", uuid.String())
						peer.Disconnect(0)
						break
					}
				default:
					log.Println("Client validation failed")
					peer.Disconnect(0)
					break
				}
			}
			if err := peer.SendPacket(packet, ev.GetChannelID()); err != nil {
				log.Printf("Couldn't send packet to client: %s\n", err.Error())
				packet.Destroy()
			}
		}
	}
	remoteHost.Destroy()
}

func main() {
	enet.Initialize()
	host, err := enet.NewHost(enet.NewListenAddress(20406), 1024, 32, 0, 0)
	if err != nil {
		log.Fatalf("Couldn't create host: %s\n", err.Error())
		return
	}

	log.Println("Server started on 20406")
	var packetChan = make(chan BridgePacket, 10)
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
				go bridgePeerToRemote(ev.GetPeer(), packetChan, closeSignal)

			case enet.EventDisconnect:
				log.Printf("Peer disconnected: %s\n", ev.GetPeer().GetAddress())
				closeSignal <- true

			case enet.EventReceive:
				packet := ev.GetPacket()
				packetChan <- BridgePacket{packet, ev.GetChannelID()}
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
