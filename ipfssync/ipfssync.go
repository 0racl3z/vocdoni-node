// Package ipfssync provides a service to synchronize IPFS datasets over a p2p network between two or more nodes
package ipfssync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/libp2p/go-libp2p-core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"gitlab.com/vocdoni/go-dvote/crypto/signature"
	"gitlab.com/vocdoni/go-dvote/data"
	"gitlab.com/vocdoni/go-dvote/log"
	"gitlab.com/vocdoni/go-dvote/net"
	"gitlab.com/vocdoni/go-dvote/tree"
	"gitlab.com/vocdoni/go-dvote/types"
	"gitlab.com/vocdoni/go-dvote/util"
)

type Message struct {
	Type     string   `json:"type"`
	Address  string   `json:"address"`
	Maddress string   `json:"mAddress"`
	NodeID   string   `json:"nodeId"`
	Hash     string   `json:"hash"`
	PinList  []string `json:"pinList"`
}

// shity function to workaround NAT problems (hope it's temporary)
func guessMyAddress(port int, id string) string {
	ip, err := util.PublicIP()
	if err != nil {
		log.Warn(err)
		return ""
	}
	if ip4 := ip.To4(); ip4 != nil {
		return fmt.Sprintf("/ip4/%s/tcp/%d/ipfs/%s", ip4, port, id)
	}
	if ip6 := ip.To16(); ip6 != nil {
		return fmt.Sprintf("/ip6/[%s]/tcp/%d/ipfs/%s", ip6, port, id)
	}
	return ""
}

func (is *IPFSsync) updatePinsTree(extraPins []string) {
	currentRoot := is.hashTree.Root()
	for _, v := range append(is.listPins(), extraPins...) {
		if len(v) > is.hashTree.MaxClaimSize() {
			log.Warnf("CID exceeds the claim size %d", len(v))
			continue
		}
		is.hashTree.AddClaim([]byte(v))
	}
	if currentRoot != is.hashTree.Root() {
		is.lastHash = currentRoot
	}
}

func (is *IPFSsync) syncPins() error {
	mkPins, err := is.hashTree.DumpPlain(is.hashTree.Root(), false)
	if err != nil {
		return err
	}
	ctx := context.TODO() // the caller should probably provide it
	pins, err := is.Storage.ListPins(ctx)
	if err != nil {
		return err
	}
	for _, v := range mkPins {
		if _, e := pins[v]; e {
			continue
		}

		log.Infof("pinning %s", v)
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := is.Storage.Pin(ctx, v); err != nil {
			log.Warn(err)
		}
	}
	return nil
}

func (is *IPFSsync) askPins(address string, hash string) error {
	var msg Message
	msg.Type = "fetch"
	msg.Address = is.myAddress
	msg.Hash = hash
	return is.unicastMsg(address, msg)
}

func (is *IPFSsync) sendPins(address string) error {
	var msg Message
	msg.Type = "fetchReply"
	msg.Address = is.myAddress
	msg.Hash = is.hashTree.Root()
	msg.PinList = is.listPins()
	return is.unicastMsg(address, msg)
}

func (is *IPFSsync) broadcastMsg(ipfsmsg Message) error {
	var msg types.Message
	d, err := json.Marshal(ipfsmsg)
	if err != nil {
		return err
	}
	msg.Data = d
	msg.TimeStamp = int32(time.Now().Unix())
	is.Transport.Send(msg)
	return nil
}

// Handle handles an Message
func (is *IPFSsync) Handle(msg Message) error {
	if msg.Address == is.myAddress {
		return nil
	}
	log.Debugf("got %+v", msg)

	switch msg.Type {
	case "hello":
		peers, err := is.Storage.CoreAPI.Swarm().Peers(is.Storage.Node.Context())
		if err != nil {
			return err
		}
		found := false
		for _, p := range peers {
			if p.ID().String() == msg.NodeID {
				found = true
			}
		}
		if !found {
			log.Infof("connecting to peer %s", msg.Maddress)
			multiAddr, err := ma.NewMultiaddr(msg.Maddress)
			if err != nil {
				return err
			}
			peerInfo, err := peer.AddrInfoFromP2pAddr(multiAddr)
			if err != nil {
				return err
			}
			return is.Storage.CoreAPI.Swarm().Connect(is.Storage.Node.Context(), *peerInfo)
		}

	case "update":
		if len(msg.Hash) > 31 && len(msg.Address) > 31 && !is.updateLock && len(is.askLock) == 0 {
			if msg.Hash != is.hashTree.Root() && msg.Hash != is.lastHash {
				log.Infof("found new hash %s from %s", msg.Hash, msg.Address)
				is.askLock = msg.Hash
				return is.askPins(msg.Address, msg.Hash)
			}
		}

	case "fetchReply":
		if len(msg.Hash) > 31 && len(msg.Address) > 31 && !is.updateLock {
			if msg.Hash != is.hashTree.Root() {
				is.updateLock = true
				log.Infof("got new pin list %s from %s", msg.Hash, msg.Address)
				is.updatePinsTree(msg.PinList)
				is.updateLock = false
				if is.askLock == msg.Hash {
					is.askLock = ""
				}
				return nil
			}
		}

	case "fetch":
		if len(msg.Hash) > 31 && len(msg.Address) > 31 {
			if msg.Hash == is.hashTree.Root() {
				log.Infof("got fetch query, sending pin list to %s", msg.Address)
				return is.sendPins(msg.Address)
			}
		}
	}

	return nil
}

func (is *IPFSsync) sendUpdate() {
	var msg Message
	msg.Type = "update"
	msg.Address = is.myAddress
	msg.Hash = is.hashTree.Root()
	if len(is.listPins()) > 0 {
		log.Debugf("current hash %s", msg.Hash)
		err := is.broadcastMsg(msg)
		if err != nil {
			log.Warn(err)
		}
	}
}

func (is *IPFSsync) sendHello() {
	var msg Message
	msg.Type = "hello"
	msg.Address = is.myAddress
	msg.Maddress = is.myMultiAddr.String()
	msg.NodeID = is.myNodeID
	err := is.broadcastMsg(msg)
	if err != nil {
		log.Warn(err)
	}
}

func (is *IPFSsync) listPins() (pins []string) {
	list, err := is.Storage.ListPins(context.TODO())
	if err != nil {
		log.Error(err)
	}
	for i := range list {
		pins = append(pins, i)
	}
	return
}

type IPFSsync struct {
	DataDir     string
	Key         string
	Port        int16
	HelloTime   int
	UpdateTime  int
	Storage     *data.IPFSHandle
	Transport   net.PSSHandle
	hashTree    tree.Tree
	Topic       string
	updateLock  bool   // TODO(mvdan): this is super racy
	askLock     string // TODO(mvdan): this is super racy
	myAddress   string
	myNodeID    string
	myMultiAddr ma.Multiaddr
	lastHash    string
}

// NewIPFSsync creates a new IPFSsync instance
func NewIPFSsync(dataDir, key string, storage data.Storage) IPFSsync {
	var is IPFSsync
	is.DataDir = dataDir
	is.Key = key
	is.Port = 4171
	is.HelloTime = 40
	is.UpdateTime = 20
	is.Storage = storage.(*data.IPFSHandle)
	return is
}

func (is *IPFSsync) unicastMsg(address string, msg Message) error {
	rawmsg, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	is.Transport.Swarm.PssPub("sym", is.Key, is.Topic, string(rawmsg), address)
	return err
}

// Start initializes and start an IPFSsync instance
func (is *IPFSsync) Start() {
	log.Infof("initializing new pin storage")
	os.RemoveAll(is.DataDir + "/ipfsSync.db")
	is.hashTree.StorageDir = is.DataDir
	if err := is.hashTree.Init("ipfsSync.db"); err != nil {
		log.Fatal(err)
	}
	is.updatePinsTree([]string{})
	log.Infof("current hash %s", is.hashTree.Root())

	var conn types.Connection
	conn.Port = int(is.Port)
	conn.Key = is.Key
	conn.Encryption = "sym"
	conn.Topic = string(signature.HashRaw(conn.Key))
	is.Topic = conn.Topic
	is.Key = conn.Key

	if err := is.Transport.Init(&conn); err != nil {
		log.Fatal(err)
	}

	msg := make(chan types.Message)
	go is.Transport.Listen(msg)
	is.myAddress = fmt.Sprintf("%x", is.Transport.Swarm.PssAddr)
	is.myNodeID = is.Storage.Node.PeerHost.ID().String()
	var err error
	is.myMultiAddr, err = ma.NewMultiaddr(guessMyAddress(4001, is.myNodeID))
	if err != nil {
		log.Fatal(err)
	}
	log.Infof("my multiaddress: %s", is.myMultiAddr)

	go func() {
		var syncMsg Message
		var err error
		for {
			d := <-msg
			err = json.Unmarshal(d.Data, &syncMsg)
			if err != nil {
				log.Warnf("cannot unmarshal message %s", err)
			} else {
				go is.Handle(syncMsg)
			}
		}
	}()

	go func() {
		for {
			is.sendHello()
			time.Sleep(time.Second * time.Duration(is.HelloTime))
		}
	}()

	go func() {
		for {
			time.Sleep(time.Second * time.Duration(is.UpdateTime))
			if !is.updateLock {
				is.updatePinsTree([]string{})
				is.sendUpdate()
			}
		}
	}()

	go func() {
		for {
			if len(is.askLock) > 0 {
				for i := 0; i < 100; i++ {
					if len(is.askLock) == 0 {
						break
					}
					time.Sleep(time.Millisecond * 100)
				}
				if len(is.askLock) > 0 {
					is.askLock = ""
					log.Warn("ask lock released due timeout")
				}
			}
			time.Sleep(time.Millisecond * 200)
		}
	}()

	for {
		err = is.syncPins()
		if err != nil {
			log.Warn(err)
		}
		time.Sleep(time.Second * 32)
	}
}
