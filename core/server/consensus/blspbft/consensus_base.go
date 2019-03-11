// Copyright The go-okchain Authors 2018,  All rights reserved.

/*
Copyright 2018 The Okchain Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

/*
* DESCRIPTION

 */

package blspbft

import (
	"encoding/hex"
	"fmt"
	"github.com/pkg/errors"
	"math"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"

	ps "github.com/ok-chain/okchain/core/server"
	logging "github.com/ok-chain/okchain/log"
	pb "github.com/ok-chain/okchain/protos"
	"github.com/ok-chain/okchain/util"
)

var logger = logging.MustGetLogger("consensusBase")

func init() {
	ps.ConsensusBackupFactory = newConsensusBackup
	ps.ConsensusLeadFactory = newConsensusLead
}

var mutex sync.Mutex

type ConsensusBase struct {
	role       ps.IRole
	peerServer *ps.PeerServer
	leader     *pb.PeerEndpoint
	state      ConsensusState_Type

	//for handling VC timeout
	leaderId      uint32
	windowClosed  bool
	vcLeaderCnt   map[string]uint32
	faultyLeaders []*pb.PeerEndpoint

	buffer []*pb.Message
}

func newConsensusBase(peer *ps.PeerServer) *ConsensusBase {
	c := &ConsensusBase{
		windowClosed: false,
		leaderId:     uint32(0),
		vcLeaderCnt:  make(map[string]uint32),
	}
	c.peerServer = peer
	c.leader = &pb.PeerEndpoint{}
	return c
}

func (c *ConsensusBase) SetIRole(irole ps.IRole) {
	c.role = irole
}

func (c *ConsensusBase) ProcessConsensusMsg(msg *pb.Message, from *pb.PeerEndpoint) error {
	util.OkChainPanic("Invalid invoke")
	return nil
}

func (c *ConsensusBase) GetCurrentLeader() *pb.PeerEndpoint {
	util.OkChainPanic("Invalid invoke")
	return nil
}

func (c *ConsensusBase) GetCurrentConsensusStage() pb.ConsensusType {
	return consensusHandler.currentType
}

func (c *ConsensusBase) SetCurrentConsensusStage(consensusType pb.ConsensusType) {
	consensusHandler.currentType = consensusType
}

/*
* 	WaitForViewChange is used as a timer to trigger ViewChange.
*	wait x seconds to collect votes in the timeout interval [ticker0, ticker1]
* 	Let t = timeout1-timeout0, t param should be specified by testnet! TODO
 */
func (c *ConsensusBase) WaitForViewChange() {
	ticker0 := time.NewTicker(time.Duration(c.peerServer.GetViewchangeTimeOut()) * time.Second)
	//c.windowClosed = false //receive votes when process block etc. XXX, should be earlier
	select {
	// waiting for channel signal when consensus finished
	case <-c.peerServer.VcChan:
		logger.Debugf("consensus finished before viewchange timeout")
		return
		// waiting for viewchange timeout
	case t := <-ticker0.C:
		logger.Debugf("viewchange timeout when consensus handling, %+v, timeout set is %ds, vcLeaderCnt is %+v, role name is %s",
			t.String(), c.peerServer.GetViewchangeTimeOut(), c.vcLeaderCnt, c.peerServer.GetCurrentRole().GetName())
		//if = 2f+1 satisfy; else = self-up a candidate leader, here is round-robin
		if leader, votes, _ := c.GetMaxVotes(); c.SatisfyTolt(votes) {
			logger.Debugf("> 2f+1, leader is %+v, votes is %v", leader, votes)
			if err := c.BroadcastVotes(leader, votes); err != nil {
				logger.Debugf("broadcastVotes err1, %s", err.Error())
				return
			}
			//No count network-latency in, XXX, start view change
			consensusHandler.StartViewChangeAsyn(c.role, leader)
			logger.Debugf("> 2f+1 end")
		} else {
			candidate, votes := c.GetNextLeader() //votes is 1
			logger.Debugf("< 2f+1, my candidate leader is %+v, votes is %v", candidate, votes)
			if err := c.BroadcastVotes(candidate, votes); err != nil {
				logger.Debugf("broadcastVotes err2, %s", err.Error())
				return
			}
			logger.Debugf("< 2f+1 end")
		}
		return
	}
}

//SatifyTolt test whether votes number is larger the 2/3
func (c *ConsensusBase) SatisfyTolt(votes uint32) bool {
	if votes < uint32(math.Floor(float64(len(c.GetPeerAdjacentList()))*ToleranceFraction))+1 {
		return false
	}
	return true
}

func (c *ConsensusBase) BroadcastVotes(leader *pb.PeerEndpoint, votes uint32) error {
	voteMsg := c.CreateVoteMsg(leader, votes)
	peerList := c.GetPeerAdjacentList()
	logger.Debugf("broadcast peerlist is %+v", peerList)
	if err := c.peerServer.Multicast(voteMsg, peerList); err != nil { //network is not stable, critical
		return ErrMultiCastMessage
	}
	logger.Debugf("propose votes, voteMsg is %+v", voteMsg)
	return nil
}

//CalMaxVotes tries to get a candidate leader when timeout
func (c *ConsensusBase) GetMaxVotes() (leader *pb.PeerEndpoint, votes uint32, err error) {
	logger.Debugf("GetMaxVotes begin")
	var (
		maxVotes = uint32(0)
		leaderPK string
	)
	for pk, votes := range c.vcLeaderCnt {
		if votes > maxVotes {
			maxVotes = votes
			leaderPK = pk
		}
	}
	logger.Debugf("max votes is %d, addr: %+v", maxVotes, leaderPK)
	var peerList = c.GetPeerAdjacentList()
	for _, peer := range peerList {
		if hex.EncodeToString(peer.Pubkey) == leaderPK {
			return peer, maxVotes, nil
		}
	}
	logger.Debugf("GetMaxVotes end")
	return nil, maxVotes, errors.New("No adjacent peers. Critical!")
}
func (c *ConsensusBase) GetNextLeader() (*pb.PeerEndpoint, uint32) {
	logger.Debugf("GetNextLeader begin")
	peerList := c.GetPeerAdjacentList()
	fmt.Printf("peerList is %+v, len is %d", peerList, len(peerList))
	if peerList == nil || uint32(len(peerList)) < c.leaderId {
		return nil, uint32(0)
	}

	fmt.Printf("leaderId is %+v, peerList len is %+v", c.leaderId, uint32(len(peerList)))

	candidate := peerList[c.leaderId%uint32(len(peerList))]
	c.AddVotes(candidate, uint32(1))

	logger.Debugf("GetNextLeader end")
	return candidate, uint32(1)
}

func (c *ConsensusBase) ProcessViewChangeVote(msg *pb.Message, peer *pb.PeerEndpoint) error {
	logger.Debugf("process view change vote msg is %+v", msg)
	var voteMsg pb.ViewChangeVote
	if err := proto.Unmarshal(msg.GetPayload(), &voteMsg); err != nil {
		logger.Debugf("unmarshal ViewChangeVote error, %+v", err)
		return err
	}
	logger.Debugf("process view change vote msg payload is %+v", voteMsg)
	//calculate votes
	c.AddVotes(voteMsg.Leader, voteMsg.Votes)

	if c.SatisfyTolt(voteMsg.Votes) {
		consensusHandler.StartViewChangeAsyn(c.role, voteMsg.Leader)
	}

	return nil
}

func (c *ConsensusBase) CreateVoteMsg(leader *pb.PeerEndpoint, votes uint32) *pb.Message {
	msg := &pb.Message{}
	msg.Type = pb.Message_Consensus_ViewChangeVote
	msg.Peer = c.peerServer.SelfNode

	var voteMsg = &pb.ViewChangeVote{
		Leader: leader,
		Votes:  votes,
	}
	data, _ := proto.Marshal(voteMsg)
	msg.Payload = data

	logger.Debugf("peer[%+v] vote for peer[%+v], votes is %d", c.peerServer.SelfNode, leader, votes)

	return msg
}

func (c *ConsensusBase) GetPeerAdjacentList() []*pb.PeerEndpoint {
	roleName := c.peerServer.GetCurrentRole().GetName()
	if roleName == "DsLead" || roleName == "DsBackup" {
		return c.peerServer.Committee
	} else if roleName == "ShardLead" || roleName == "ShardBackup" {
		return c.peerServer.ShardingNodes
	}
	return nil
}

func (c *ConsensusBase) AddVotes(candidate *pb.PeerEndpoint, votes uint32) {
	var pk = hex.EncodeToString(candidate.Pubkey)
	mutex.Lock()
	c.vcLeaderCnt[pk] += votes
	mutex.Unlock()
	logger.Debugf("peer %v add votes %v", pk, votes)
}
