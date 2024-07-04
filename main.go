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

func bridgePeerToRemote(peer enet.Peer, packetChan <-chan BridgePacket, closeSignal <-chan bool) {
	remoteHost, err := enet.NewHost(nil, 32, 256, 0, 0)
	if err != nil {
		log.Fatalf("Couldn't create host: %s", err.Error())
		return
	}
	// Connect the client host to the server
	remote, err := remoteHost.Connect(enet.NewAddress("127.0.0.1", 8888), 256, 0)
	if err != nil {
		log.Printf("Couldn't connect to server: %s\n", err.Error())
		return
	}

	var closed = false

	go func() {
		for {
			select {
			case bridgePacket := <-packetChan:
				if err := remote.SendPacket(bridgePacket.packet, bridgePacket.channelID); err != nil {
					log.Printf("Couldn't send packet to server: %s\n", err.Error())
					bridgePacket.packet.Destroy()
				}
			case <-closeSignal:
				closed = true
			}
		}
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
