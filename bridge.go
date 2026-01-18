package blastoff

import (
	"log"

	"github.com/Heavenlode/Blastoff/internal/enet"
	"github.com/google/uuid"
)

// Command types for the bridge
type bridgeCommand int

const (
	cmdSendPacket bridgeCommand = iota
	cmdInitialConnect
	cmdRedirect
	cmdClose
)

type bridgeCmd struct {
	cmd       bridgeCommand
	packet    *enet.Packet
	channelID uint8
	data      []byte // For initial token
	uuid      uuid.UUID
}

func (server *BlastoffServer) bridgePeerToRemote(peer *enet.Peer, peerIncomingPacket <-chan bridgePacket, closeSignal <-chan bool) {
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

	// Channel for commands to the main loop (which handles all ENet operations)
	cmdChan := make(chan bridgeCmd, 100)

	// Goroutine to receive packets from the client and convert to commands
	go func() {
		for {
			select {
			case peerPacket := <-peerIncomingPacket:
				if peerValidated {
					cmdChan <- bridgeCmd{
						cmd:       cmdSendPacket,
						packet:    peerPacket.packet,
						channelID: peerPacket.channelID,
					}
				} else {
					// Initial connection packet with token - send to main loop
					data := peerPacket.packet.GetData()
					peerPacket.packet.Destroy()
					cmdChan <- bridgeCmd{
						cmd:  cmdInitialConnect,
						data: data,
					}
				}
			case <-closeSignal:
				cmdChan <- bridgeCmd{cmd: cmdClose}
				return
			}
		}
	}()

	// Main loop - ALL ENet operations happen here (single goroutine)
remoteLoop:
	for {
		if closed {
			log.Println("[Bridge] Closed, exiting remote loop")
			break
		}

		// Check for commands (non-blocking)
		select {
		case cmd := <-cmdChan:
			switch cmd.cmd {
			case cmdInitialConnect:
				peerToken = cmd.data
				remotePeer, err = remoteHost.Connect(defaultRemote, 250, 0)
				if err != nil {
					log.Printf("Couldn't connect to server: %s\n", err.Error())
					break remoteLoop
				}
			case cmdSendPacket:
				if remotePeer != nil && peerValidated {
					data := cmd.packet.GetData()
					flags := cmd.packet.GetFlags()
					cmd.packet.Destroy()
					if err := remotePeer.SendBytes(data, cmd.channelID, flags); err != nil {
						log.Printf("Couldn't send packet to server: %s\n", err.Error())
					}
					remoteHost.Flush()
				} else {
					cmd.packet.Destroy()
				}
			case cmdRedirect:
				if remotePeer != nil {
					remotePeer.Disconnect(0)
				}
				remotePeer, err = remoteHost.Connect(server.remoteAddressMap[cmd.uuid], 0, 0)
				if err != nil {
					log.Printf("Couldn't connect to server: %s\n", err.Error())
					break remoteLoop
				}
				err = remotePeer.SendBytes(peerToken, RemoteAdminChannelId, enet.PacketFlagReliable)
				if err != nil {
					log.Printf("Couldn't send token to server: %s\n", err.Error())
					break remoteLoop
				}
				remoteHost.Flush()
			case cmdClose:
				closed = true
				break remoteLoop
			}
		default:
			// No command, continue to service
		}

		if remoteHost == nil {
			log.Println("[Bridge] Remote host is nil, exiting")
			break
		}

		ev := remoteHost.Service(10)
		if ev.GetType() == enet.EventNone {
			continue
		}

		switch ev.GetType() {
		case enet.EventConnect:
			if remotePeer == nil {
				log.Println("[Bridge] Remote peer is nil on connect event")
				break remoteLoop
			}
			err = remotePeer.SendBytes(peerToken, RemoteAdminChannelId, enet.PacketFlagReliable)
			if err != nil {
				log.Printf("Couldn't send token to server: %s\n", err.Error())
				break remoteLoop
			}
			remoteHost.Flush()

		case enet.EventDisconnect, enet.EventDisconnectTimeout:
			log.Println("[Bridge] Remote disconnected or timed out")
			break remoteLoop

		case enet.EventReceive:
			packet := ev.GetPacket()
			if ev.GetChannelID() == RemoteAdminChannelId {
				var data = packet.GetData()
				packet.Destroy()
				switch ServerCommandFlag(data[0]) {
				case ServerCommandNewInstance:
					if len(data) < 37 {
						log.Println("Invalid new instance data")
						break remoteLoop
					}
					newUUID, parseErr := uuid.FromBytes(data[1:37])
					if parseErr != nil {
						log.Printf("Couldn't parse UUID: %s\n", parseErr.Error())
						break remoteLoop
					}
					log.Printf("Spawning new instance: %s\n", newUUID.String())
				case ServerCommandValidateClient:
					peerValidated = true
				case ServerCommandRedirectClient:
					if len(data) < 37 {
						log.Println("Invalid redirect data length")
						break remoteLoop
					}
					redirectUUID, parseErr := uuid.FromBytes(data[1:37])
					if parseErr != nil {
						log.Printf("Couldn't parse UUID: %s\n", parseErr.Error())
						break remoteLoop
					}
					if _, ok := server.remoteAddressMap[redirectUUID]; !ok {
						log.Printf("Remote host ID %s not found during redirect.", redirectUUID.String())
						break remoteLoop
					}
					// Handle redirect in this goroutine (safe - all ENet ops in main loop)
					if remotePeer != nil {
						remotePeer.Disconnect(0)
					}
					remotePeer, err = remoteHost.Connect(server.remoteAddressMap[redirectUUID], 0, 0)
					if err != nil {
						log.Printf("Couldn't connect to server: %s\n", err.Error())
						break remoteLoop
					}
				default:
					log.Println("Client validation failed")
					break remoteLoop
				}
				continue
			} else {
				if !peerValidated {
					log.Println("Client not validated.")
					break remoteLoop
				}
				data := packet.GetData()
				flags := packet.GetFlags()
				packet.Destroy()
				if err := peer.SendBytes(data, ev.GetChannelID(), flags); err != nil {
					log.Printf("Couldn't send packet to client: %s\n", err.Error())
				}
			}
		}
	}

	if peer != nil {
		peer.Disconnect(0)
	}
	if remoteHost != nil {
		remoteHost.Destroy()
	}
	log.Println("[Bridge] Bridge closed")
}
