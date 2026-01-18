package blastoff

import (
	"log"

	"github.com/Heavenlode/Blastoff/internal/enet"
	"github.com/google/uuid"
)

func (server *BlastoffServer) bridgePeerToRemote(peer *enet.Peer, peerIncomingPacket <-chan bridgePacket, closeSignal <-chan bool) {
	log.Printf("[Bridge] Starting bridge for peer %s\n", peer.GetAddress())
	remoteHost, err := enet.NewHost(nil, 100, 0, 0, 0)
	var remotePeer *enet.Peer
	if err != nil {
		log.Fatalf("Couldn't create host: %s", err.Error())
		return
	}
	remoteHost.CompressWithRangeCoder()
	var closed = false
	var peerValidated = false
	var peerToken []byte

	var redirectChan = make(chan uuid.UUID, 1)

	go func() {
		// Handle messages to and from the client
		log.Printf("[Bridge] Waiting for packets from client...\n")
	peerLoop:
		for {
			select {
			case uuid := <-redirectChan:
				// The server has indicated that the client should be redirected to another server
				log.Printf("[Bridge] Redirecting to %s\n", uuid.String())
				remotePeer.Disconnect(0)
				remotePeer, err = remoteHost.Connect(server.remoteAddressMap[uuid], 0, 0)
				if err != nil {
					log.Printf("Couldn't connect to server: %s\n", err.Error())
					break peerLoop
				}
				err = remotePeer.SendBytes(peerToken, RemoteAdminChannelId, enet.PacketFlagReliable)
				if err != nil {
					log.Printf("Couldn't send token to server: %s\n", err.Error())
					break peerLoop
				}
			case peerPacket := <-peerIncomingPacket:
				log.Printf("[Bridge] Received packet from client on channel %d (validated: %v)\n", peerPacket.channelID, peerValidated)
				if peerValidated {
					// If the peer is already validated, we simply send the packet to the remote
					if err := remotePeer.SendPacket(peerPacket.packet, peerPacket.channelID); err != nil {
						log.Printf("Couldn't send packet to server: %s\n", err.Error())
						peerPacket.packet.Destroy()
					}
					break
				}
				// Initialize the connection with the remote host.
				// First, the client sends a packet:
				// The first part is the UUID of the remote host they wish to connect to
				// The second part is the token that the remote host will use to verify the connection
				var data = peerPacket.packet.GetData()
				log.Printf("[Bridge] Got initial token from client (%d bytes), connecting to remote %s:%d\n", len(data), defaultRemote.String(), defaultRemote.GetPort())
				peerPacket.packet.Destroy()
				// if len(data) <= 36 {
				// 	log.Println("Invalid packet data length")
				// 	break peerLoop
				// }
				// uuid, err := uuid.Parse(string(data[:36]))
				// if err != nil {
				// 	log.Printf("Couldn't parse UUID: %s\n", err.Error())
				// 	break peerLoop
				// }
				// if _, ok := server.remoteAddressMap[uuid]; !ok {
				// 	log.Printf("Remote host ID %s not found.", uuid.String())
				// 	break peerLoop
				// }
				peerToken = data
				// Now we connect to the remote
				remotePeer, err = remoteHost.Connect(defaultRemote, 250, 0)
				if err != nil {
					log.Printf("Couldn't connect to server: %s\n", err.Error())
					break peerLoop
				}
			case <-closeSignal:
				break peerLoop
			}
		}
		closed = true
	}()

	// Handle incoming messages from the remote
	// These are messages which the remote sends to the client
	// Unless coming from channel RemoteAdminChannelId, then it's a message for the Blastoff server
	log.Printf("[Bridge] Starting remote event loop\n")
remoteLoop:
	for {
		if closed {
			log.Printf("[Bridge] Client connection closed, exiting remote loop\n")
			break
		}
		ev, result := remoteHost.Service(10)
		if result < 0 {
			log.Printf("[Bridge] Remote service error: %d\n", result)
			continue
		}
		if ev.GetType() == enet.EventNone {
			continue
		}
		log.Printf("[Bridge] Remote event: %d (result=%d)\n", ev.GetType(), result)
		switch ev.GetType() {
		case enet.EventConnect:
			log.Printf("[Bridge] Connected to remote server! Sending token (%d bytes)\n", len(peerToken))
			err = remotePeer.SendBytes(peerToken, RemoteAdminChannelId, enet.PacketFlagReliable)
			if err != nil {
				log.Printf("Couldn't send token to server: %s\n", err.Error())
				break remoteLoop
			}
			log.Printf("[Bridge] Token sent to remote\n")
		case enet.EventDisconnect:
			log.Printf("[Bridge] Disconnected from remote\n")
			break remoteLoop

		case enet.EventReceive:
			packet := ev.GetPacket()
			log.Printf("[Bridge] Received packet from remote on channel %d (%d bytes)\n", ev.GetChannelID(), packet.GetLength())
			if ev.GetChannelID() == RemoteAdminChannelId {
				var data = packet.GetData()
				packet.Destroy()
				// This is a special communication packet from the server
				// The first byte indicates the type of packet
				log.Printf("[Bridge] Admin channel message, command: %d\n", data[0])
				switch ServerCommandFlag(data[0]) {
				case ServerCommandNewInstance:
					// The server is redirecting the client to another server
					// The data contains the UUID of the server to redirect to
					if len(data) < 37 {
						log.Println("Invalid new instance data")
						break remoteLoop
					}
					uuid, err := uuid.FromBytes(data[1:37])
					if err != nil {
						log.Printf("Couldn't parse UUID: %s\n", err.Error())
						break remoteLoop
					}
					log.Printf("Spawning new instance: %s\n", uuid.String())
				case ServerCommandValidateClient:
					log.Printf("[Bridge] Client validated by remote server!\n")
					peerValidated = true
				case ServerCommandRedirectClient:
					// The client is being redirected to another server
					// The data contains the UUID of the server to redirect to
					if len(data) < 37 {
						log.Println("Invalid redirect data length")
						break remoteLoop
					}
					uuid, err := uuid.FromBytes(data[1:37])
					if err != nil {
						log.Printf("Couldn't parse UUID: %s\n", err.Error())
						break remoteLoop
					}
					if _, ok := server.remoteAddressMap[uuid]; !ok {
						log.Printf("Remote host ID %s not found during redirect.", uuid.String())
						break remoteLoop
					}
				default:
					log.Println("Client validation failed")
					break remoteLoop
				}
				continue
			} else {
				if !peerValidated {
					// This is a bug. The server should not be sending messages through this stream before the client is validated.
					log.Println("Client not validated.")
					break remoteLoop
				}
				if err := peer.SendPacket(packet, ev.GetChannelID()); err != nil {
					log.Printf("Couldn't send packet to client: %s\n", err.Error())
					packet.Destroy()
				}
			}
		}
	}
	peer.Disconnect(0)
	remoteHost.Destroy()
}
