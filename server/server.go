package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"paxos/api"
	"paxos/constants"
	"paxos/logger"
	"sort"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// Current Ballot of server
// should be in initialized to (1,serverID)
type BallotNumber struct {
	ballotVal int
	serverID  int
}

// Clien send m <sender, reciever, amt>
type ClientRequestTxn struct {
	sender   string
	reciever string
	amount   int
}

// Need to check timestamp before executing a request
type ClientMessage struct {
	txn       *ClientRequestTxn
	timestamp int64 // TODO change to datetime later
}

// Structure of a single record that will be stored in this servers log
type LogRecord struct {
	seqNum      int
	ballot      *BallotNumber
	txn         *ClientRequestTxn
	isCommitted bool
	isExecuted  bool
}

type LogStore struct {
	lock    sync.Mutex
	records map[int]*LogRecord
}

// Just stores client balances and process newly committed transactions
type StateMachine struct {
	lock                  sync.Mutex
	vault                 map[string]int
	lastExecutedCommitNum int
	executionStatus       map[int]chan bool
	execPool              chan struct{}
}

type Client struct {
	lastRequestTimeStamp int64
	lastResponse         *api.Reply
}

// maps unique client id to their request and last served timestamp
type ClientImpl struct {
	lock       sync.Mutex
	clientList map[string]*Client
}

// The main server struct
type ServerImpl struct {
	lock          sync.Mutex
	id            int
	ballot        *BallotNumber
	seqNum        int
	leaderTime    *time.Timer
	state         int
	clientManager *ClientImpl
	port          string
	api.UnimplementedClientServerTxnsServer
	api.UnimplementedPaxosPrintInfoServer
}

/*
Check for AB lock sequence to avoid deadlock ( Like contentManagger and stateManager issue)

Questions to be answered:
		1. Do we route stale client requests to current leader? What if
		 the current leader didn't server that message ? Maybe re-route it to the old leader who served the message
		ANS: NO RIGHT that request might not have qourum
		2. when Leader asks followers to replicate can we send false in RPC for faster processing? Or if
			follower doesn't accept the log do we need to wait for the RPC to timeout(not ideal)
    	3. If a txn sender balance less than amt do we still commit transaction and only respond as failed?
		4. In normal flow if state transitions, from leader to anything else what happens to current reqs being processed

TODO:
1) Use constant for log level, after project complete
2) Dynamically set new users balance to 10
3) In client store connections of servers - Done
4) In server store connections for other nodes
6) In client on failure needs to braodcast to all nodes
*/

var server *ServerImpl
var logStore *LogStore
var sm *StateMachine
var logs *zap.SugaredLogger

func startServer(id int, t time.Duration) {

	logs = logger.InitLogger(id, true)

	logs.Debug("Enter")
	defer logs.Debug("Exit")

	logStore = &LogStore{
		records: make(map[int]*LogRecord),
	}

	cm := &ClientImpl{ // TODO leader forward
		clientList: make(map[string]*Client),
	}
	// TODO Chnage how we assign balance
	sm = &StateMachine{
		vault:                 make(map[string]int),
		lastExecutedCommitNum: 0,
		executionStatus:       make(map[int]chan bool),
		execPool:              make(chan struct{}, 1),
	}

	server = &ServerImpl{
		id:            id,
		ballot:        &BallotNumber{ballotVal: 1, serverID: id},
		seqNum:        0,
		leaderTime:    time.NewTimer(t),
		state:         constants.Leader, // Change to Leader for testing
		clientManager: cm,
	}
	server.port = constants.ServerPorts[id]
	sm.startExec()
	logs.Infof("Server Initialized successfully with serverId: %d and timer duration: %d", id, t)
}

func main() {

	if len(os.Args) < 2 {
		log.Fatalf("Server ID must be provided as a command-line argument")
	}
	serverIdString := os.Args[1]
	serverId, err := strconv.Atoi(serverIdString)
	if err != nil {
		log.Fatalf("Invalid Server ID: %s", serverIdString)
	}

	leaderTimeout := constants.LEADER_TIMEOUT_SECONDS * time.Second
	startServer(serverId, leaderTimeout)
	logs.Infof("Server-%d is up and running. Waiting for requests on port %s", serverId, server.port)

	// Starting grpc server
	addr := fmt.Sprintf(":%s", server.port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		logs.Fatalf("Failed to listen on port %s: %v", server.port, err)
	}

	grpcServer := grpc.NewServer()
	api.RegisterClientServerTxnsServer(grpcServer, server)
	// TODO MAYBE DEDICATED SERVER FOR PRINT
	api.RegisterPaxosPrintInfoServer(grpcServer, server)
	logs.Infof("gRPC server listening at %s", server.port)
	err = grpcServer.Serve(lis)
	if err != nil {
		logs.Fatalf("Failed to serve gRPC: %v", err)
	}
}

func stateTransition(from int, to int) {

	// TODO
}

func isValidStateTransition(from int, to int) {
	//TODO
}

/*
	txn in a , b , c
	lets say concensus for a take a while b,c committed but not executed
	we need to wait for a to commit
*/

func (s *ServerImpl) Request(ctx context.Context, in *api.Message) (*api.Reply, error) {

	logs.Debug("Entry")
	defer logs.Debug("Exit")

	logs.Infof("Received transaction from %s to %s for amount %d", in.Sender, in.Receiver, in.Amount)

	// TODO leader forward
	// TODO caching client request and request timestamp check
	// We'll allow self transfer, its a redundant log but the system should handle it like a normal txn
	// Exact once semantics before the leader check other wise on failed req, all nodes will flood Leader
	s.clientManager.lock.Lock()
	client, ok := s.clientManager.clientList[in.ClientId]
	if !ok {
		client = &Client{}
		s.clientManager.clientList[in.ClientId] = client
		logs.Infof("First request from client %s. Creating client cache", in.ClientId)
	}

	if in.GetTimestamp() < client.lastRequestTimeStamp {
		logs.Warnf("Recieved a stale request from client-%s", in.ClientId)
		s.clientManager.lock.Unlock()
		return &api.Reply{Result: false, ServerId: int32(s.id), Error: "Invalid stale request"}, nil
	}

	if in.GetTimestamp() == client.lastRequestTimeStamp {
		logs.Infof("Recieved duplicate request from client-%s", in.ClientId)
		resp := client.lastResponse
		s.clientManager.lock.Unlock()
		if resp == nil {
			resp = &api.Reply{Result: false, ServerId: int32(s.id), Error: "Failed to send cached resp"}
		}

		return resp, nil
	}
	client.lastRequestTimeStamp = in.GetTimestamp()
	client.lastResponse = nil
	s.clientManager.lock.Unlock()

	s.lock.Lock()
	if s.state != constants.Leader {
		logs.Warnf("Cannot process this transaction, not current cluster leader")
		// TODO forwarding to current leader
		s.lock.Unlock()
		return &api.Reply{Result: false, ServerId: int32(s.id)}, nil
	}
	s.seqNum++
	currSeqNum := s.seqNum
	record := &LogRecord{
		seqNum: currSeqNum,
		ballot: s.ballot,
		txn: &ClientRequestTxn{
			sender:   in.Sender,
			reciever: in.Receiver,
			amount:   int(in.Amount),
		},
		isCommitted: false,
	}
	// need to unlock here otherwise we are doing sequential processing
	s.lock.Unlock()

	logStore.append(record)
	execChannel := sm.registerSignal(currSeqNum)

	// TODO Concesus PRC HERE

	logStore.markCommitted(currSeqNum)
	sm.applyTxn()

	logs.Infof("Waiting for seqNum %d to be executed", currSeqNum)
	res := <-execChannel
	logs.Infof("Execution completed for seqNum %d, txn success: %t ", currSeqNum, res)

	s.lock.Lock()
	reply := &api.Reply{
		BallotVal: int32(s.ballot.ballotVal),
		ServerId:  int32(s.id),
		Timestamp: in.GetTimestamp(),
		ClientId:  in.GetClientId(),
		Result:    res,
	}
	s.lock.Unlock()

	s.clientManager.lock.Lock()
	defer s.clientManager.lock.Unlock()
	client, ok = s.clientManager.clientList[in.ClientId]
	if ok && client.lastRequestTimeStamp == in.GetTimestamp() {
		client.lastResponse = reply
		logs.Infof("Cached reply for client-%s with request timestamp: %d", in.ClientId, in.GetTimestamp())
	}
	return reply, nil

}

func (sm *StateMachine) startExec() {
	logs.Debug("Start")
	defer logs.Debug("Exit")

	go func() {
		for range sm.execPool {
			sm.execCommitLogs()
		}
	}()
}

func (sm *StateMachine) execCommitLogs() {
	logs.Debug("Start")
	defer logs.Debug("Exit")

	logStore.lock.Lock()
	sm.lock.Lock()
	defer sm.lock.Unlock()
	defer logStore.lock.Unlock()

	for {
		nextCommitIdx := sm.lastExecutedCommitNum + 1
		logToApply, ok := logStore.records[nextCommitIdx]

		if !ok || !logToApply.isCommitted {
			break
		}
		sender := logToApply.txn.sender
		reciever := logToApply.txn.reciever

		if _, ok := sm.vault[sender]; !ok {
			sm.vault[sender] = constants.INITIAL_BALANCE
			logs.Infof("Detected new user, Initializing Balance for User: %s with %d", sender, constants.INITIAL_BALANCE)
		}
		if _, ok := sm.vault[reciever]; !ok {
			sm.vault[reciever] = constants.INITIAL_BALANCE
			logs.Infof("Detected new user, Initializing Balance for User: %s with %d", reciever, constants.INITIAL_BALANCE)
		}

		var res bool
		if sm.vault[sender] >= logToApply.txn.amount {
			sm.vault[sender] -= logToApply.txn.amount
			sm.vault[reciever] += logToApply.txn.amount
			res = true
			logs.Infof("Successfully applied txn with commitIdx %d and segNum %d", nextCommitIdx, logToApply.seqNum)
		} else {
			logs.Warnf("Insufficient Balance for txn with commitIdx %d and segNum %d", nextCommitIdx, logToApply.seqNum)
		}
		sm.lastExecutedCommitNum++
		logToApply.isExecuted = true
		execChan, ok := sm.executionStatus[logToApply.seqNum]
		if ok {
			execChan <- res
			close(execChan)
			delete(sm.executionStatus, logToApply.seqNum)
		}

	}

}

func (ls *LogStore) append(record *LogRecord) {
	logs.Debug("Enter")
	defer logs.Debug("Exit")

	ls.lock.Lock()
	defer ls.lock.Unlock()
	ls.records[record.seqNum] = record
	logs.Infof("Added record to log with seqNum %d", record.seqNum)

}

func (sm *StateMachine) registerSignal(seqNum int) chan bool {
	logs.Debug("Enter")
	defer logs.Debug("Exit")

	sm.lock.Lock()
	defer sm.lock.Unlock()
	execChan := make(chan bool, 1)
	sm.executionStatus[seqNum] = execChan
	return execChan
}

func (ls *LogStore) markCommitted(seqNum int) {
	logs.Debug("Enter")
	defer logs.Debug("Exit")

	ls.lock.Lock()
	defer ls.lock.Unlock()

	record, ok := ls.records[seqNum]
	if ok {
		record.isCommitted = true
		logs.Infof("Log with seqNum %d commited", seqNum)
	} else {
		logs.Warnf("No record found with seqNum: %d", seqNum)
	}
}

func (sm *StateMachine) applyTxn() {

	logs.Debug("Enter")
	defer logs.Debug("Exit")

	select {
	case sm.execPool <- struct{}{}:
	default:
	}

}

func (s *ServerImpl) PrintLog(ctx context.Context, in *api.Blank) (*api.Logs, error) {
	logs.Debug("Enter")
	defer logs.Debug("Exit")

	logStore.lock.Lock()
	defer logStore.lock.Unlock()

	keys := make([]int, 0, len(logStore.records))
	for k := range logStore.records {
		keys = append(keys, k)
	}
	sort.Ints(keys)

	entries := make([]*api.LogRecord, 0, len(logStore.records))
	for _, seqNum := range keys {
		record := logStore.records[seqNum]
		entry := &api.LogRecord{
			SeqNum:      int32(record.seqNum),
			BallotVal:   int32(record.ballot.ballotVal),
			ServerId:    int32(record.ballot.serverID),
			Sender:      record.txn.sender,
			Receiver:    record.txn.reciever,
			Amount:      int32(record.txn.amount),
			IsCommitted: record.isCommitted,
		}
		entries = append(entries, entry)
	}
	return &api.Logs{Logs: entries}, nil
}

func (s *ServerImpl) PrintDB(ctx context.Context, in *api.Blank) (*api.Vault, error) {
	logs.Debug("Enter")
	defer logs.Debug("Exit")

	sm.lock.Lock()
	defer sm.lock.Unlock()

	vaultCopy := make(map[string]int32)
	for k, v := range sm.vault {
		vaultCopy[k] = int32(v)
	}
	return &api.Vault{Vault: vaultCopy}, nil
}

func (s *ServerImpl) PrintStatus(ctx context.Context, in *api.RequestInfo) (*api.Status, error) {
	logs.Debug("Enter")
	defer logs.Debug("Exit")

	seqNum := int(in.GetSeqNum())
	logStore.lock.Lock()
	defer logStore.lock.Unlock()
	record, ok := logStore.records[seqNum]
	if !ok {
		return &api.Status{Status: api.TxnState_NOSTATUS}, nil
	}
	if record.isExecuted {
		return &api.Status{Status: api.TxnState_EXECUTED}, nil
	}
	if record.isCommitted {
		return &api.Status{Status: api.TxnState_COMMITTED}, nil
	}
	return &api.Status{Status: api.TxnState_ACCEPTED}, nil
}
