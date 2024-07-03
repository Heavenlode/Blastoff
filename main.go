package main

import (
	"log"
	"sync"

	"github.com/codecat/go-enet"
)

type BridgePacket struct {
	packet    enet.Packet
	channelID uint8
}

func bridgePeerToRemote(peer enet.Peer, packetChan <-chan BridgePacket) {
	func() {
		remoteHost, err := enet.NewHost(nil, 1, 1, 0, 0)
		if err != nil {
			log.Fatalln("Couldn't create host: %s", err.Error())
			return
		}
		// Connect the client host to the server
		remote, err := remoteHost.Connect(enet.NewAddress("127.0.0.1", 8888), 1, 0)
		if err != nil {
			log.Println("Couldn't connect: %s", err.Error())
			return
		}

		go func() {
			for true {
				select {
				case bridgePacket := <-packetChan:
					remote.SendPacket(bridgePacket.packet, bridgePacket.channelID)
					bridgePacket.packet.Destroy()
				}
			}
		}()

		// The event loop
		for true {
			// Wait until the next event
			ev := remoteHost.Service(1000)

			// Send a ping if we didn't get any event
			if ev.GetType() == enet.EventNone {
				continue
			}

			switch ev.GetType() {
			case enet.EventConnect: // We connected to the server
				log.Println("Peer bridged to the server!")

			case enet.EventDisconnect: // We disconnected from the server
				log.Println("Lost connection to the server!")

			case enet.EventReceive: // The server sent us data
				packet := ev.GetPacket()
				peer.SendPacket(packet, ev.GetChannelID())
				packet.Destroy()
			}
		}
		remoteHost.Destroy()
	}()
}

func main() {
	// Initialize enet
	enet.Initialize()

	// Create a host listening on 0.0.0.0:8095
	host, err := enet.NewHost(enet.NewListenAddress(20406), 32, 1, 0, 0)
	if err != nil {
		log.Fatalln("Couldn't create host: %s", err.Error())
		return
	}

	log.Println("Server started on 20406")
	var wg sync.WaitGroup
	wg.Add(1)
	var packetChan = make(chan BridgePacket, 1)
	// The event loop
	go func() {
		for true {
			// Wait until the next event
			ev := host.Service(1000)

			// Do nothing if we didn't get any event
			if ev.GetType() == enet.EventNone {
				continue
			}

			switch ev.GetType() {
			case enet.EventConnect: // A new peer has connected
				log.Println("New peer connected: %s", ev.GetPeer().GetAddress())
				go bridgePeerToRemote(ev.GetPeer(), packetChan)

			case enet.EventDisconnect: // A connected peer has disconnected
				log.Println("Peer disconnected: %s", ev.GetPeer().GetAddress())

			case enet.EventReceive: // A peer sent us some data
				// Get the packet
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
