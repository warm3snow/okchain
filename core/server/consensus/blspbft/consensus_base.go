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
	"math"
	"reflect"
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
	leaderId      uint16
	windowClosed  bool
	vcLeaderCnt   map[string]uint16
	faultyLeaders []*pb.PeerEndpoint

	buffer []*pb.Message
}

func newConsensusBase(peer *ps.PeerServer) *ConsensusBase {
	c := &ConsensusBase{
		windowClosed: false,
		leaderId:     uint16(0),
		vcLeaderCnt:  make(map[string]uint16),
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
		// * 检查VCLeaderCnt，找出最大的cnt，如果存在$(cnt>f+1)的节点，则设置候选leader为该节点
		// * 否则发送leader投票信息
	case t := <-ticker0.C:
		logger.Debugf("viewchange ticker0 timeout when consensus handling, %+v, timeout set is %ds, vcLeaderCnt is %+v, role name is %s",
			t.String(), c.peerServer.GetViewchangeTimeOut(), c.vcLeaderCnt, c.peerServer.GetCurrentRole().GetName())

		//2f+1 satisfy
		if newLeader := c.GetViewChangeLeader(); newLeader != nil {
			err := consensusHandler.StartViewChangeAsyn(c.role, newLeader)
			if err == nil {
				logger.Debugf("Async view change, my view change leader is %+v", newLeader)
				//c.windowClosed = true
				c.leaderId = 0
				c.vcLeaderCnt = make(map[string]uint16)
			} else {
				logger.Debugf("Async view change err: %+v", err)
			}

		} else { //if votes < 2f+1, go to vote and change leader by round-robin
			logger.Debugf("Syn view change, start to vote. no available 2f+1 votes leader")
			c.leaderId += uint16(1)
			_, err := c.BroadcastVotes()
			if err != nil {
				logger.Debugf("BroadcastVotes err: %+v", err)
			}
			//consensusHandler.StartViewChangeAsyn(c.role, candidate)
		}
	}
}

func (c *ConsensusBase) BroadcastVotes() (*pb.PeerEndpoint, error) {
	candidate, voteMsg, peerList := c.CreateVoteMsg(c.leaderId)
	logger.Debugf("candidate is %+v, voteMsg is %+v, peerList is %+v", candidate, voteMsg, peerList)
	if err := c.peerServer.Multicast(voteMsg, peerList); err != nil {
		return nil, err
	}
	logger.Debugf("propose votes, voteMsg is %+v", voteMsg)
	//consensusHandler.StartViewChange(c.role)
	return candidate, nil
}

//GetViewChangeLeader is used to get a candidate leader when timeout, if votes > 2f+1 exists, admit this candidate
func (c *ConsensusBase) GetViewChangeLeader() *pb.PeerEndpoint {
	var (
		candidateAddr string
		maxVotes      uint16 = 0
	)
	//find the node having most votes, which is the candidate leader
	for addr, votes := range c.vcLeaderCnt {
		if votes > maxVotes {
			maxVotes = votes
			candidateAddr = addr
		}
	}
	logger.Debugf("max votes is %d, addr: %+v", maxVotes, candidateAddr)
	var peerList = c.GetPeerAdjacentList()
	if peerList == nil || maxVotes < uint16(math.Floor(float64(len(peerList))*ToleranceFraction))+1 {
		return nil
	}

	var candidate *pb.PeerEndpoint
	for _, peer := range peerList {
		if reflect.DeepEqual(peer.Coinbase, []byte(candidateAddr)) {
			candidate = peer
			break
		}
	}
	return candidate
}

func (c *ConsensusBase) ProcessViewChangeVote(msg *pb.Message, peer *pb.PeerEndpoint) error {
	//if c.windowClosed {
	//	logger.Debugf("ViewChangeVote colleting window closed")
	//	return nil
	//}
	logger.Debugf("process view change vote msg is %+v", msg)
	var candidate pb.PeerEndpoint
	if err := proto.Unmarshal(msg.GetPayload(), &candidate); err != nil {
		logger.Debugf("unmarshal ViewChangeVote error, %+v", err)
		return err
	}
	//calculate votes
	mutex.Lock()
	c.RefreshVotes(string(candidate.Coinbase))
	mutex.Unlock()

	return nil
}

func (c *ConsensusBase) CreateVoteMsg(leaderId uint16) (*pb.PeerEndpoint, *pb.Message, []*pb.PeerEndpoint) {
	msg := &pb.Message{}
	msg.Type = pb.Message_Consensus_ViewChangeVote
	msg.Peer = c.peerServer.SelfNode

	//select candidate
	peerList := c.GetPeerAdjacentList()
	var (
		candidate *pb.PeerEndpoint
		data      []byte
	)
	if peerList != nil {
		candidate = peerList[int(leaderId)%len(peerList)]
		data, _ = proto.Marshal(candidate)
	}
	msg.Payload = data

	//add vote locally before send vote
	mutex.Lock()
	c.RefreshVotes(string(candidate.Coinbase))
	mutex.Unlock()

	logger.Debugf("peer[%+v] vote for peer[%d][%+v]", c.peerServer.SelfNode, leaderId, candidate)

	return candidate, msg, peerList
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

func(c *ConsensusBase) RefreshVotes(address string){
	if _, ok := c.vcLeaderCnt[address]; !ok {
		c.vcLeaderCnt[address] = 1
	} else {
		c.vcLeaderCnt[address]++
	}
}