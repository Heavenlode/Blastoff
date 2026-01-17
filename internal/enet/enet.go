package enet

/*
#cgo CFLAGS: -I${SRCDIR}
#cgo LDFLAGS: -lm
#cgo windows LDFLAGS: -lws2_32 -lwinmm

#define ENET_IMPLEMENTATION
#include "enet.h"
#include <stdlib.h>
#include <string.h>
*/
import "C"
import (
	"errors"
	"fmt"
	"unsafe"
)

// EventType represents the type of an ENet event
type EventType int

const (
	EventNone              EventType = C.ENET_EVENT_TYPE_NONE
	EventConnect           EventType = C.ENET_EVENT_TYPE_CONNECT
	EventDisconnect        EventType = C.ENET_EVENT_TYPE_DISCONNECT
	EventReceive           EventType = C.ENET_EVENT_TYPE_RECEIVE
	EventDisconnectTimeout EventType = C.ENET_EVENT_TYPE_DISCONNECT_TIMEOUT
)

// PacketFlag represents packet flags
type PacketFlag uint32

const (
	PacketFlagNone                 PacketFlag = C.ENET_PACKET_FLAG_NONE
	PacketFlagReliable             PacketFlag = C.ENET_PACKET_FLAG_RELIABLE
	PacketFlagUnsequenced          PacketFlag = C.ENET_PACKET_FLAG_UNSEQUENCED
	PacketFlagNoAllocate           PacketFlag = C.ENET_PACKET_FLAG_NO_ALLOCATE
	PacketFlagUnreliableFragmented PacketFlag = C.ENET_PACKET_FLAG_UNRELIABLE_FRAGMENTED
	PacketFlagInstant              PacketFlag = C.ENET_PACKET_FLAG_INSTANT
	PacketFlagUnthrottled          PacketFlag = C.ENET_PACKET_FLAG_UNTHROTTLED
)

// Address wraps an ENet address
type Address struct {
	cAddr C.ENetAddress
}

// Host wraps an ENet host
type Host struct {
	cHost *C.ENetHost
}

// Peer wraps an ENet peer
type Peer struct {
	cPeer *C.ENetPeer
}

// Packet wraps an ENet packet
type Packet struct {
	cPacket *C.ENetPacket
}

// Event wraps an ENet event
type Event struct {
	cEvent C.ENetEvent
}

// Initialize initializes ENet
func Initialize() error {
	if C.enet_initialize() != 0 {
		return errors.New("failed to initialize ENet")
	}
	return nil
}

// Deinitialize shuts down ENet
func Deinitialize() {
	C.enet_deinitialize()
}

// NewAddress creates a new address from an IP string and port
func NewAddress(ip string, port uint16) (Address, error) {
	addr := Address{}
	addr.cAddr.port = C.uint16_t(port)

	cIP := C.CString(ip)
	defer C.free(unsafe.Pointer(cIP))

	if C.enet_address_set_ip(&addr.cAddr, cIP) != 0 {
		// Try as hostname if IP fails
		if C.enet_address_set_hostname(&addr.cAddr, cIP) != 0 {
			return addr, fmt.Errorf("failed to set address: %s", ip)
		}
	}

	return addr, nil
}

// GetPort returns the port of the address
func (a Address) GetPort() uint16 {
	return uint16(a.cAddr.port)
}

// String returns the IP address as a string
func (a Address) String() string {
	buf := make([]byte, C.ENET_HOST_SIZE)
	C.enet_address_get_ip(&a.cAddr, (*C.char)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)))
	// Find null terminator
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

// NewHost creates a new ENet host
// address can be nil to create a client-only host
func NewHost(address *Address, peerCount, channelLimit int, inBandwidth, outBandwidth uint32) (*Host, error) {
	var cAddr *C.ENetAddress
	if address != nil {
		cAddr = &address.cAddr
	}

	// bufferSize parameter (last arg) - use 0 for default
	cHost := C.enet_host_create(cAddr, C.size_t(peerCount), C.size_t(channelLimit), C.uint32_t(inBandwidth), C.uint32_t(outBandwidth), 0)
	if cHost == nil {
		return nil, errors.New("failed to create ENet host")
	}

	return &Host{cHost: cHost}, nil
}

// Destroy destroys the host
func (h *Host) Destroy() {
	if h.cHost != nil {
		C.enet_host_destroy(h.cHost)
		h.cHost = nil
	}
}

// Connect initiates a connection to a remote host
func (h *Host) Connect(address Address, channelCount int, data uint32) (*Peer, error) {
	cPeer := C.enet_host_connect(h.cHost, &address.cAddr, C.size_t(channelCount), C.uint32_t(data))
	if cPeer == nil {
		return nil, errors.New("failed to initiate connection")
	}
	return &Peer{cPeer: cPeer}, nil
}

// Service waits for events and dispatches them
func (h *Host) Service(timeout uint32) Event {
	ev := Event{}
	C.enet_host_service(h.cHost, &ev.cEvent, C.uint32_t(timeout))
	return ev
}

// Flush sends any queued packets
func (h *Host) Flush() {
	C.enet_host_flush(h.cHost)
}

// CompressWithRangeCoder is a no-op in the nxrighthere ENet fork
// The modified protocol doesn't use the same compression as standard ENet
func (h *Host) CompressWithRangeCoder() error {
	// No-op: nxrighthere ENet doesn't have range coder compression
	return nil
}

// GetType returns the event type
func (e Event) GetType() EventType {
	return EventType(e.cEvent._type)
}

// GetPeer returns the peer associated with the event
func (e Event) GetPeer() *Peer {
	if e.cEvent.peer == nil {
		return nil
	}
	return &Peer{cPeer: e.cEvent.peer}
}

// GetPacket returns the packet associated with the event
func (e Event) GetPacket() *Packet {
	if e.cEvent.packet == nil {
		return nil
	}
	return &Packet{cPacket: e.cEvent.packet}
}

// GetChannelID returns the channel ID of the event
func (e Event) GetChannelID() uint8 {
	return uint8(e.cEvent.channelID)
}

// GetData returns the data field of the event
func (e Event) GetData() uint32 {
	return uint32(e.cEvent.data)
}

// Send sends a packet to the peer on the specified channel
func (p *Peer) Send(channelID uint8, packet *Packet) error {
	if C.enet_peer_send(p.cPeer, C.uint8_t(channelID), packet.cPacket) != 0 {
		return errors.New("failed to send packet")
	}
	return nil
}

// SendBytes creates a packet from data and sends it
func (p *Peer) SendBytes(data []byte, channelID uint8, flags PacketFlag) error {
	packet := NewPacket(data, flags)
	return p.Send(channelID, packet)
}

// SendPacket sends an existing packet to the peer
func (p *Peer) SendPacket(packet *Packet, channelID uint8) error {
	return p.Send(channelID, packet)
}

// Disconnect gracefully disconnects from the peer
func (p *Peer) Disconnect(data uint32) {
	C.enet_peer_disconnect(p.cPeer, C.uint32_t(data))
}

// DisconnectNow forcefully disconnects from the peer
func (p *Peer) DisconnectNow(data uint32) {
	C.enet_peer_disconnect_now(p.cPeer, C.uint32_t(data))
}

// Reset forcefully resets the peer
func (p *Peer) Reset() {
	C.enet_peer_reset(p.cPeer)
}

// GetAddress returns the address of the peer as a string
func (p *Peer) GetAddress() string {
	buf := make([]byte, C.ENET_HOST_SIZE)
	C.enet_peer_get_ip(p.cPeer, (*C.char)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)))
	// Find null terminator
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

// GetID returns a unique ID for this peer (useful for map keys)
func (p *Peer) GetID() uint32 {
	return uint32(C.enet_peer_get_id(p.cPeer))
}

// GetState returns the current state of the peer
func (p *Peer) GetState() int {
	return int(C.enet_peer_get_state(p.cPeer))
}

// Pointer returns the underlying C pointer as uintptr for use as map key
func (p *Peer) Pointer() uintptr {
	return uintptr(unsafe.Pointer(p.cPeer))
}

// NewPacket creates a new packet with the given data and flags
func NewPacket(data []byte, flags PacketFlag) *Packet {
	var dataPtr unsafe.Pointer
	if len(data) > 0 {
		dataPtr = unsafe.Pointer(&data[0])
	}

	cPacket := C.enet_packet_create(dataPtr, C.size_t(len(data)), C.uint32_t(flags))
	if cPacket == nil {
		return nil
	}

	return &Packet{cPacket: cPacket}
}

// GetData returns the packet data as a byte slice
func (p *Packet) GetData() []byte {
	if p.cPacket == nil {
		return nil
	}

	length := int(C.enet_packet_get_length(p.cPacket))
	if length == 0 {
		return nil
	}

	data := C.enet_packet_get_data(p.cPacket)
	return C.GoBytes(data, C.int(length))
}

// GetLength returns the length of the packet data
func (p *Packet) GetLength() int {
	if p.cPacket == nil {
		return 0
	}
	return int(C.enet_packet_get_length(p.cPacket))
}

// Destroy destroys the packet
func (p *Packet) Destroy() {
	if p.cPacket != nil {
		C.enet_packet_destroy(p.cPacket)
		p.cPacket = nil
	}
}
