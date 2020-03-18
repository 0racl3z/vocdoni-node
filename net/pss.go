package net

import (
	"time"

	"gitlab.com/vocdoni/go-dvote/log"
	"gitlab.com/vocdoni/go-dvote/swarm"
	"gitlab.com/vocdoni/go-dvote/types"
)

type PSSHandle struct {
	Conn      *types.Connection
	Swarm     *swarm.SimpleSwarm
	BootNodes []string
}

func (p *PSSHandle) Init(c *types.Connection) error {
	p.Conn = c
	sn := new(swarm.SimpleSwarm)
	if len(p.BootNodes) == 0 {
		p.BootNodes = swarm.VocdoniBootnodes
	}
	err := sn.InitPSS(p.BootNodes)
	if err != nil {
		return err
	}
	sn.PssSub(p.Conn.Encryption, p.Conn.Key, p.Conn.Topic)
	p.Swarm = sn
	return nil
}

func (p *PSSHandle) Listen(reciever chan<- types.Message) {
	var msg types.Message
	for {
		pssMessage := <-p.Swarm.PssTopics[p.Conn.Topic].Delivery
		ctx := new(types.PssContext)
		ctx.Topic = p.Conn.Topic
		ctx.PeerAddress = pssMessage.Peer.String()
		msg.Data = pssMessage.Msg
		msg.TimeStamp = int32(time.Now().Unix())
		msg.Context = ctx
		reciever <- msg
	}
}

func (p *PSSHandle) Address() string {
	return p.Conn.Address
}

func (p *PSSHandle) SetBootnodes(bootnodes []string) {
	p.BootNodes = bootnodes
}

func (p *PSSHandle) Send(msg types.Message) {
	err := p.Swarm.PssPub(p.Conn.Encryption, p.Conn.Key, p.Conn.Topic, string(msg.Data), p.Conn.Address)
	if err != nil {
		log.Warnf("PSS send error: %s", err)
	}
}

func (p *PSSHandle) SendUnicast(address string, msg types.Message) {
	p.Swarm.PssPub("sym", p.Conn.Key, p.Conn.Topic, string(msg.Data), address)
}
