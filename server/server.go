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
	timestamp int // TODO change to datetime later
}

// Structure of a single record that will be stored in this servers log
type LogRecord struct {
	seqNum int
	ballot *BallotNumber
	txn    *ClientRequestTxn
}

// Just stores client balances and process newly committed transactions
type StateMachine struct {
	lock                  sync.Mutex
	vault                 map[string]int
	lastExecutedCommitNum int
	queue                 map[int]*ClientRequestTxn // Client is waiting for this to execute, how to send resp in same reeq rpc ? Maybe timer ticks every second to check if lastExecutedSeqNum >= seqNum and then respond
}

type Response struct{} //TODO

type Client struct {
	lastTimeStampExecuted int               // TODO make it datetime
	requestCache          map[int]*Response //TODO create response struct

}

// maps unique client id to their request and last served timestamp
type ClientImpl struct {
	lock       sync.Mutex
	clientList map[int]*Client
}

// The main server struct
type ServerImpl struct {
	lock          sync.Mutex
	id            int
	ballot        *BallotNumber
	seqNum        int
	lastCommitIdx int
	logs          []*LogRecord
	leaderTime    *time.Timer
	stateMachine  *StateMachine
	state         int
	api.UnimplementedClientServerTxnsServer
}

// Check for AB lock sequence to avoid deadlock ( Like contentManagger and stateManager issue)

// Questions to be answered:
// 		1. Do we route stale client requests to current leader? What if
// 		 the current leader didn't server that message ? Maybe re-route it to the old leader who served the message
//		ANS: NO RIGHT that request might not have qourum

var server *ServerImpl
var clientManager *ClientImpl
var logs *zap.SugaredLogger
var port string

func startServer(id int, t time.Duration) {

	logs = logger.InitLogger(id, true)

	logs.Debug("Enter")
	defer logs.Debug("Exit")

	clientManager = &ClientImpl{
		clientList: make(map[int]*Client),
	}

	sm := &StateMachine{
		vault:                 make(map[string]int),
		lastExecutedCommitNum: 0,
		queue:                 make(map[int]*ClientRequestTxn),
	}

	server = &ServerImpl{
		id:            id,
		ballot:        &BallotNumber{ballotVal: 1, serverID: id},
		seqNum:        0,
		lastCommitIdx: 0,
		logs:          make([]*LogRecord, 0),
		leaderTime:    time.NewTimer(t),
		stateMachine:  sm,
		state:         constants.Follower,
	}
	port = constants.ServerPorts[id]
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
	logs.Infof("Server-%d is up and running. Waiting for requests on port %s", serverId, port)

	// Starting grpc server
	addr := fmt.Sprintf(":%s", port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		logs.Fatalf("Failed to listen on port %s: %v", port, err)
	}

	grpcServer := grpc.NewServer()
	api.RegisterClientServerTxnsServer(grpcServer, server)

	logs.Infof("gRPC server listening at %s", port)
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

func (s *ServerImpl) Request(ctx context.Context, in *api.Message) (*api.Reply, error) {

	logs.Debug("Entry")
	defer logs.Debug("Exit")

	logs.Infof("Received transaction from %s to %s for amount %d", in.Sender, in.Receiver, in.Amount)

	// TODO leader check
	//
	reply := &api.Reply{
		BallotVal: int32(s.ballot.ballotVal),
		ServerId:  int32(s.id),
		Timestamp: in.GetTimestamp(),
		ClientId:  in.GetClientId(),
		Result:    true, // Assume success for now
	}

	return reply, nil
}
