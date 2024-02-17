/* SPDX-License-Identifier: MIT
 *
  * Copyright (C) 2017-2023 WireGuard LLC. All Rights Reserved.
  * Copyright (C) 2023 Synthesis Labs. All Rights Reserved.
 */

package device

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/syntlabs/cyanide-go/conn"
	"github.com/syntlabs/cyanide-go/ipc"
	"github.com/syntlabs/cyanide-go/ratelimiter"
	"github.com/syntlabs/cyanide-go/rwcancel"
	"github.com/syntlabs/cyanide-go/tun"
	"github.com/tevino/abool/v2"
)

type Device struct {
	state struct {
		// state holds the device's state. It is accessed atomically.
		// Use the device.deviceState method to read it.
		// device.deviceState does not acquire the mutex, so it captures only a snapshot.
		// During state transitions, the state variable is updated before the device itself.
		// The state is thus either the current state of the device or
		// the intended future state of the device.
		// For example, while executing a call to Up, state will be deviceStateUp.
		// There is no guarantee that that intended future state of the device
		// will become the actual state; Up can fail.
		// The device can also change state multiple times between time of check and time of use.
		// Unsynchronized uses of state must therefore be advisory/best-effort only.
		state atomic.Uint32 // actually a deviceState, but typed uint32 for convenience
		// stopping blocks until all inputs to Device have been closed.
		stopping sync.WaitGroup
		// mu protects state changes.
		sync.Mutex
	}

	net struct {
		stopping sync.WaitGroup
		sync.RWMutex
		bind          conn.Bind // bind interface
		netlinkCancel *rwcancel.RWCancel
		port          uint16 // listening port
		fwmark        uint32 // mark value (0 = disabled)
		brokenRoaming bool
	}

	staticIdentity struct {
		sync.RWMutex
		privateKey NoisePrivateKey
		publicKey  NoisePublicKey
	}

	peers struct {
		sync.RWMutex // protects keyMap
		keyMap       map[NoisePublicKey]*Peer
	}

	rate struct {
		underLoadUntil atomic.Int64
		limiter        ratelimiter.Ratelimiter
	}

	allowedips    AllowedIPs
	indexTable    IndexTable
	cookieChecker CookieChecker

	pool struct {
		inboundElementsContainer  *WaitPool
		outboundElementsContainer *WaitPool
		messageBuffers            *WaitPool
		inboundElements           *WaitPool
		outboundElements          *WaitPool
	}

	queue struct {
		encryption *outboundQueue
		decryption *inboundQueue
		handshake  *handshakeQueue
	}

	tun struct {
		device tun.Device
		mtu    atomic.Int32
	}

	ipcMutex sync.RWMutex
	closed   chan struct{}
	log      *Logger

	isASecOn abool.AtomicBool
	aSecMux  sync.RWMutex
	aSecConf  aSecConfType
}

type aSecConfType struct {
	isSet                      bool
	junkPacketCount            int
	junkPacketMinSize          int
	junkPacketMaxSize          int
	initPacketJunkSize         int
	responsePacketJunkSize     int
	initPacketMagicHeader      uint32
	responsePacketMagicHeader  uint32
	underloadPacketMagicHeader uint32
	transportPacketMagicHeader uint32
}

// deviceState represents the state of a Device.
// There are three states: down, up, closed.
// Transitions:
//
//	down -----+
//	  ↑↓      ↓
//	  up -> closed
type deviceState uint32

//go:generate go run golang.org/x/tools/cmd/stringer -type deviceState -trimprefix=deviceState
const (
	deviceStateDown deviceState = iota
	deviceStateUp
	deviceStateClosed
)

// deviceState returns device.state.state as a deviceState
// See those docs for how to interpret this value.
func (device *Device) deviceState() deviceState {
	return deviceState(device.state.state.Load())
}

// isClosed reports whether the device is closed (or is closing).
// See device.state.state comments for how to interpret this value.
func (device *Device) isClosed() bool {
	return device.deviceState() == deviceStateClosed
}

// isUp reports whether the device is up (or is attempting to come up).
// See device.state.state comments for how to interpret this value.
func (device *Device) isUp() bool {
	return device.deviceState() == deviceStateUp
}

// Must hold device.peers.Lock()
func removePeerLocked(device *Device, peer *Peer, key NoisePublicKey) {
	// stop routing and processing of packets
	device.allowedips.RemoveByPeer(peer)
	peer.Stop()

	// remove from peer map
	delete(device.peers.keyMap, key)
}

// changeState attempts to change the device state to match want.
func (device *Device) changeState(want deviceState) (err error) {
	device.state.Lock()
	defer device.state.Unlock()
	old := device.deviceState()
	if old == deviceStateClosed {
		// once closed, always closed
		device.log.Verbosef("Interface closed, ignored requested state %s", want)
		return nil
	}
	switch want {
	case old:
		return nil
	case deviceStateUp:
		device.state.state.Store(uint32(deviceStateUp))
		err = device.upLocked()
		if err == nil {
			break
		}
		fallthrough // up failed; bring the device all the way back down
	case deviceStateDown:
		device.state.state.Store(uint32(deviceStateDown))
		errDown := device.downLocked()
		if err == nil {
			err = errDown
		}
	}
	device.log.Verbosef("Interface state was %s, requested %s, now %s", old, want, device.deviceState())

	return
}

// upLocked attempts to bring the device up and reports whether it succeeded.
// The caller must hold device.state.mu and is responsible for updating device.state.state.
func (device *Device) upLocked() error {
	if err := device.BindUpdate(); err != nil {
		device.log.Errorf("Unable to update bind: %v", err)
		return err
	}

	// The IPC set operation waits for peers to be created before calling Start() on them,
	// so if there's a concurrent IPC set request happening, we should wait for it to complete.
	device.ipcMutex.Lock()
	defer device.ipcMutex.Unlock()

	device.peers.RLock()
	for _, peer := range device.peers.keyMap {
		peer.Start()
		if peer.persistentKeepaliveInterval.Load() > 0 {
			peer.SendKeepalive()
		}
	}
	device.peers.RUnlock()
	return nil
}

// downLocked attempts to bring the device down.
// The caller must hold device.state.mu and is responsible for updating device.state.state.
func (device *Device) downLocked() error {
	err := device.BindClose()
	if err != nil {
		device.log.Errorf("Bind close failed: %v", err)
	}

	device.peers.RLock()
	for _, peer := range device.peers.keyMap {
		peer.Stop()
	}
	device.peers.RUnlock()
	return err
}

func (device *Device) Up() error {
	return device.changeState(deviceStateUp)
}

func (device *Device) Down() error {
	return device.changeState(deviceStateDown)
}

func (device *Device) IsUnderLoad() bool {
	// check if currently under load
	now := time.Now()
	underLoad := len(device.queue.handshake.c) >= QueueHandshakeSize/8
	if underLoad {
		device.rate.underLoadUntil.Store(now.Add(UnderLoadAfterTime).UnixNano())
		return true
	}
	// check if recently under load
	return device.rate.underLoadUntil.Load() > now.UnixNano()
}

func (device *Device) SetPrivateKey(sk NoisePrivateKey) error {
	// lock required resources

	device.staticIdentity.Lock()
	defer device.staticIdentity.Unlock()

	if sk.Equals(device.staticIdentity.privateKey) {
		return nil
	}

	device.peers.Lock()
	defer device.peers.Unlock()

	lockedPeers := make([]*Peer, 0, len(device.peers.keyMap))
	for _, peer := range device.peers.keyMap {
		peer.handshake.mutex.RLock()
		lockedPeers = append(lockedPeers, peer)
	}

	// remove peers with matching public keys

	publicKey := sk.publicKey()
	for key, peer := range device.peers.keyMap {
		if peer.handshake.remoteStatic.Equals(publicKey) {
			peer.handshake.mutex.RUnlock()
			removePeerLocked(device, peer, key)
			peer.handshake.mutex.RLock()
		}
	}

	// update key material

	device.staticIdentity.privateKey = sk
	device.staticIdentity.publicKey = publicKey
	device.cookieChecker.Init(publicKey)

	// do static-static DH pre-computations

	expiredPeers := make([]*Peer, 0, len(device.peers.keyMap))
	for _, peer := range device.peers.keyMap {
		handshake := &peer.handshake
		handshake.precomputedStaticStatic, _ = device.staticIdentity.privateKey.sharedSecret(handshake.remoteStatic)
		expiredPeers = append(expiredPeers, peer)
	}

	for _, peer := range lockedPeers {
		peer.handshake.mutex.RUnlock()
	}
	for _, peer := range expiredPeers {
		peer.ExpireCurrentKeypairs()
	}

	return nil
}

func NewDevice(tunDevice tun.Device, bind conn.Bind, logger *Logger) *Device {
	device := new(Device)
	device.state.state.Store(uint32(deviceStateDown))
	device.closed = make(chan struct{})
	device.log = logger
	device.net.bind = bind
	device.tun.device = tunDevice
	mtu, err := device.tun.device.MTU()
	if err != nil {
		device.log.Errorf("Trouble determining MTU, assuming default: %v", err)
		mtu = DefaultMTU
	}
	device.tun.mtu.Store(int32(mtu))
	device.peers.keyMap = make(map[NoisePublicKey]*Peer)
	device.rate.limiter.Init()
	device.indexTable.Init()

	device.PopulatePools()

	// create queues

	device.queue.handshake = newHandshakeQueue()
	device.queue.encryption = newOutboundQueue()
	device.queue.decryption = newInboundQueue()

	// start workers

	cpus := runtime.NumCPU()
	device.state.stopping.Wait()
	device.queue.encryption.cn.Add(cpus) // One for each RoutineHandshake
	for i := 0; i < cpus; i++ {
		go device.RoutineEncryption(i + 1)
		go device.RoutineDecryption(i + 1)
		go device.RoutineHandshake(i + 1)
	}

	device.state.stopping.Add(1)      // RoutineReadFromTUN
	device.queue.encryption.cn.Add(1) // RoutineReadFromTUN
	go device.RoutineReadFromTUN()
	go device.RoutineTUNEventReader()

	return device
}

// BatchSize returns the BatchSize for the device as a whole which is the max of
// the bind batch size and the tun batch size. The batch size reported by device
// is the size used to construct memory pools, and is the allowed batch size for
// the lifetime of the device.
func (device *Device) BatchSize() int {
	size := device.net.bind.BatchSize()
	dSize := device.tun.device.BatchSize()
	if size < dSize {
		size = dSize
	}
	return size
}

func (device *Device) LookupPeer(pk NoisePublicKey) *Peer {
	device.peers.RLock()
	defer device.peers.RUnlock()

	return device.peers.keyMap[pk]
}

func (device *Device) RemovePeer(key NoisePublicKey) {
	device.peers.Lock()
	defer device.peers.Unlock()
	// stop peer and remove from routing

	peer, ok := device.peers.keyMap[key]
	if ok {
		removePeerLocked(device, peer, key)
	}
}

func (device *Device) RemoveAllPeers() {
	device.peers.Lock()
	defer device.peers.Unlock()

	for key, peer := range device.peers.keyMap {
		removePeerLocked(device, peer, key)
	}

	device.peers.keyMap = make(map[NoisePublicKey]*Peer)
}

func (device *Device) Close() {
	device.state.Lock()
	defer device.state.Unlock()
	device.ipcMutex.Lock()
	defer device.ipcMutex.Unlock()
	if device.isClosed() {
		return
	}
	device.state.state.Store(uint32(deviceStateClosed))
	device.log.Verbosef("Device closing")

	device.tun.device.Close()
	device.downLocked()

	// Remove peers before closing queues,
	// because peers assume that queues are active.
	device.RemoveAllPeers()

	// We kept a reference to the encryption and decryption queues,
	// in case we started any new peers that might write to them.
	// No new peers are coming; we are done with these queues.
	device.queue.encryption.cn.Done()
	device.queue.decryption.cn.Done()
	device.queue.handshake.cn.Done()
	device.state.stopping.Wait()

	device.rate.limiter.Close()

	device.log.Verbosef("Device closed")
	close(device.closed)
}

func (device *Device) Wait() chan struct{} {
	return device.closed
}

func (device *Device) SendKeepalivesToPeersWithCurrentKeypair() {
	if !device.isUp() {
		return
	}

	device.peers.RLock()
	for _, peer := range device.peers.keyMap {
		peer.keypairs.RLock()
		sendKeepalive := peer.keypairs.current != nil && !peer.keypairs.current.created.Add(RejectAfterTime).Before(time.Now())
		peer.keypairs.RUnlock()
		if sendKeepalive {
			peer.SendKeepalive()
		}
	}
	device.peers.RUnlock()
}

// closeBindLocked closes the device's net.bind.
// The caller must hold the net mutex.
func closeBindLocked(device *Device) error {
	var err error
	netc := &device.net
	if netc.netlinkCancel != nil {
		netc.netlinkCancel.Cancel()
	}
	if netc.bind != nil {
		err = netc.bind.Close()
	}
	netc.stopping.Wait()
	return err
}

func (device *Device) Bind() conn.Bind {
	device.net.Lock()
	defer device.net.Unlock()
	return device.net.bind
}

func (device *Device) BindSetMark(mark uint32) error {
	device.net.Lock()
	defer device.net.Unlock()

	// check if modified
	if device.net.fwmark == mark {
		return nil
	}

	// update fwmark on existing bind
	device.net.fwmark = mark
	if device.isUp() && device.net.bind != nil {
		if err := device.net.bind.SetMark(mark); err != nil {
			return err
		}
	}

	// clear cached source addresses
	device.peers.RLock()
	for _, peer := range device.peers.keyMap {
		peer.markEndpointSrcForClearing()
	}
	device.peers.RUnlock()

	return nil
}

func (device *Device) BindUpdate() error {
	device.net.Lock()
	defer device.net.Unlock()

	// close existing sockets
	if err := closeBindLocked(device); err != nil {
		return err
	}

	// open new sockets
	if !device.isUp() {
		return nil
	}

	// bind to new port
	var err error
	var recvFns []conn.ReceiveFunc
	netc := &device.net

	recvFns, netc.port, err = netc.bind.Open(netc.port)
	if err != nil {
		netc.port = 0
		return err
	}

	netc.netlinkCancel, err = device.startRouteListener(netc.bind)
	if err != nil {
		netc.bind.Close()
		netc.port = 0
		return err
	}

	// set fwmark
	if netc.fwmark != 0 {
		err = netc.bind.SetMark(netc.fwmark)
		if err != nil {
			return err
		}
	}

	// clear cached source addresses
	device.peers.RLock()
	for _, peer := range device.peers.keyMap {
		peer.markEndpointSrcForClearing()
	}
	device.peers.RUnlock()

	// start receiving routines
	device.net.stopping.Add(len(recvFns))
	device.queue.decryption.cn.Add(len(recvFns)) // each RoutineReceiveIncoming goroutine writes to device.queue.decryption
	device.queue.handshake.cn.Add(len(recvFns))  // each RoutineReceiveIncoming goroutine writes to device.queue.handshake
	batchSize := netc.bind.BatchSize()
	for _, fn := range recvFns {
		go device.RoutineReceiveIncoming(batchSize, fn)
	}

	device.log.Verbosef("UDP bind has been updated")
	return nil
}

func (device *Device) BindClose() error {
	device.net.Lock()
	err := closeBindLocked(device)
	device.net.Unlock()
	return err
}

func (device *Device) isAdvancedSecurityOn() bool {
	return device.isASecOn.IsSet()
}

func (device *Device) handlePostConfig(tempASecConf *aSecConfType) (err error) {

	if !tempASecConf.isSet {
		return err
	}

	isASecOn := false
	device.aSecMux.Lock()
	if tempASecConf.junkPacketCount < 0 {
		err = ipcErrorf(
			ipc.IpcErrorInvalid,
			"JunkPacketCount should be non negative",
		)
	}
	device.aSecConf.junkPacketCount = tempASecConf.junkPacketCount
	if tempASecConf.junkPacketCount != 0 {
		isASecOn = true
	}

	device.aSecConf.junkPacketMinSize = tempASecConf.junkPacketMinSize
	if tempASecConf.junkPacketMinSize != 0 {
		isASecOn = true
	}

	if device.aSecConf.junkPacketCount > 0 &&
		tempASecConf.junkPacketMaxSize == tempASecConf.junkPacketMinSize {

		tempASecConf.junkPacketMaxSize++ 
	}

	if tempASecConf.junkPacketMaxSize >= MaxSegmentSize {
		device.aSecConf.junkPacketMinSize = 0
		device.aSecConf.junkPacketMaxSize = 1
		if err != nil {
			err = ipcErrorf(
				ipc.IpcErrorInvalid,
				"JunkPacketMaxSize: %d; should be smaller than maxSegmentSize: %d; %w",
				tempASecConf.junkPacketMaxSize,
				MaxSegmentSize,
				err,
			)
		} else {
			err = ipcErrorf(
				ipc.IpcErrorInvalid,
				"JunkPacketMaxSize: %d; should be smaller than maxSegmentSize: %d",
				tempASecConf.junkPacketMaxSize,
				MaxSegmentSize,
			)
		}
	} else if tempASecConf.junkPacketMaxSize < tempASecConf.junkPacketMinSize {
		if err != nil {
			err = ipcErrorf(
				ipc.IpcErrorInvalid,
				"maxSize: %d; should be greater than minSize: %d; %w",
				tempASecConf.junkPacketMaxSize,
				tempASecConf.junkPacketMinSize,
				err,
			)
		} else {
			err = ipcErrorf(
				ipc.IpcErrorInvalid,
				"maxSize: %d; should be greater than minSize: %d",
				tempASecConf.junkPacketMaxSize,
				tempASecConf.junkPacketMinSize,
			)
		}
	} else {
		device.aSecConf.junkPacketMaxSize = tempASecConf.junkPacketMaxSize
	}

	if tempASecConf.junkPacketMaxSize != 0 {
		isASecOn = true
	}

	if MessageInitiationSize+tempASecConf.initPacketJunkSize >= MaxSegmentSize {
		if err != nil {
			err = ipcErrorf(
				ipc.IpcErrorInvalid,
				`init header size(148) + junkSize:%d; should be smaller than maxSegmentSize: %d; %w`,
				tempASecConf.initPacketJunkSize,
				MaxSegmentSize,
				err,
			)
		} else {
			err = ipcErrorf(
				ipc.IpcErrorInvalid,
				`init header size(148) + junkSize:%d; should be smaller than maxSegmentSize: %d`,
				tempASecConf.initPacketJunkSize,
				MaxSegmentSize,
			)
		}
	} else {
		device.aSecConf.initPacketJunkSize = tempASecConf.initPacketJunkSize
	}

	if tempASecConf.initPacketJunkSize != 0 {
		isASecOn = true
	}

	if MessageResponseSize+tempASecConf.responsePacketJunkSize >= MaxSegmentSize {
		if err != nil {
			err = ipcErrorf(
				ipc.IpcErrorInvalid,
				`response header size(92) + junkSize:%d; should be smaller than maxSegmentSize: %d; %w`,
				tempASecConf.responsePacketJunkSize,
				MaxSegmentSize,
				err,
			)
		} else {
			err = ipcErrorf(
				ipc.IpcErrorInvalid,
				`response header size(92) + junkSize:%d; should be smaller than maxSegmentSize: %d`,
				tempASecConf.responsePacketJunkSize,
				MaxSegmentSize,
			)
		}
	} else {
		device.aSecConf.responsePacketJunkSize = tempASecConf.responsePacketJunkSize
	}

	if tempASecConf.responsePacketJunkSize != 0 {
		isASecOn = true
	}

	if tempASecConf.initPacketMagicHeader > 4 {
		isASecOn = true
		device.log.Verbosef("UAPI: Updating init_packet_magic_header")
		device.aSecConf.initPacketMagicHeader = tempASecConf.initPacketMagicHeader
		MessageInitiationType = device.aSecConf.initPacketMagicHeader
	} else {
		device.log.Verbosef("UAPI: Using default init type")
		MessageInitiationType = 1
	}

	if tempASecConf.responsePacketMagicHeader > 4 {
		isASecOn = true
		device.log.Verbosef("UAPI: Updating response_packet_magic_header")
		device.aSecConf.responsePacketMagicHeader = tempASecConf.responsePacketMagicHeader
		MessageResponseType = device.aSecConf.responsePacketMagicHeader
	} else {
		device.log.Verbosef("UAPI: Using default response type")
		MessageResponseType = 2
	}

	if tempASecConf.underloadPacketMagicHeader > 4 {
		isASecOn = true
		device.log.Verbosef("UAPI: Updating underload_packet_magic_header")
		device.aSecConf.underloadPacketMagicHeader = tempASecConf.underloadPacketMagicHeader
		MessageCookieReplyType = device.aSecConf.underloadPacketMagicHeader
	} else {
		device.log.Verbosef("UAPI: Using default underload type")
		MessageCookieReplyType = 3
	}

	if tempASecConf.transportPacketMagicHeader > 4 {
		isASecOn = true
		device.log.Verbosef("UAPI: Updating transport_packet_magic_header")
		device.aSecConf.transportPacketMagicHeader = tempASecConf.transportPacketMagicHeader
		MessageTransportType = device.aSecConf.transportPacketMagicHeader
	} else {
		device.log.Verbosef("UAPI: Using default transport type")
		MessageTransportType = 4
	}

	isSameMap := map[uint32]bool{}
	isSameMap[MessageInitiationType] = true
	isSameMap[MessageResponseType] = true
	isSameMap[MessageCookieReplyType] = true
	isSameMap[MessageTransportType] = true

	if len(isSameMap) != 4 {
		if err != nil {
			err = ipcErrorf(
				ipc.IpcErrorInvalid,
				`magic headers should differ; got: init:%d; recv:%d; unde:%d; tran:%d; %w`,
				MessageInitiationType,
				MessageResponseType,
				MessageCookieReplyType,
				MessageTransportType,
				err,
			)
		} else {
			err = ipcErrorf(
				ipc.IpcErrorInvalid,
				`magic headers should differ; got: init:%d; recv:%d; unde:%d; tran:%d`,
				MessageInitiationType,
				MessageResponseType,
				MessageCookieReplyType,
				MessageTransportType,
			)
		}
	}

	newInitSize := MessageInitiationSize + device.aSecConf.initPacketJunkSize
	newResponseSize := MessageResponseSize + device.aSecConf.responsePacketJunkSize

	if newInitSize == newResponseSize {
		if err != nil {
			err = ipcErrorf(
				ipc.IpcErrorInvalid,
				`new init size:%d; and new response size:%d; should differ; %w`,
				newInitSize,
				newResponseSize,
				err,
			)
		} else {
			err = ipcErrorf(
				ipc.IpcErrorInvalid,
				`new init size:%d; and new response size:%d; should differ`,
				newInitSize,
				newResponseSize,
			)
		}
	} else {
		packetSizeToMsgType = map[int]uint32{
			newInitSize:            MessageInitiationType,
			newResponseSize:        MessageResponseType,
			MessageCookieReplySize: MessageCookieReplyType,
			MessageTransportSize:   MessageTransportType,
		}

		msgTypeToJunkSize = map[uint32]int{
			MessageInitiationType:  device.aSecConf.initPacketJunkSize,
			MessageResponseType:    device.aSecConf.responsePacketJunkSize,
			MessageCookieReplyType: 0,
			MessageTransportType:   0,
		}
	}

	device.isASecOn.SetTo(isASecOn)
	device.aSecMux.Unlock()

	return err
}