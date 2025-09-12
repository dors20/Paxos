package main

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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
}

// Check for AB lock sequence to avoid deadlock ( Like contentManagger and stateManager issue)

// Questions to be answered:
// 		1. Do we route stale client requests to current leader? What if
// 		 the current leader didn't server that message ? Maybe re-route it to the old leader who served the message
//		ANS: NO RIGHT that request might not have qourum

var server *ServerImpl
var clientManager *ClientImpl
var loggerMain *zap.Logger
var logger *zap.SugaredLogger

// https://dev.to/ronnymedina/golang-logging-configuration-with-zap-practical-implementation-tips-6g7
func initLogger(serverId int) {

	fileName := fmt.Sprintf("server_%d.log", serverId)
	logFile, err := os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open lof file %s %v", fileName, err)
	}

	config := zap.NewProductionEncoderConfig()
	config.EncodeTime = zapcore.ISO8601TimeEncoder
	encoder := zapcore.NewJSONEncoder(config)
	fileSync := zapcore.AddSync(logFile)

	core := zapcore.NewCore(encoder, fileSync, zap.DebugLevel)

	loggerMain = zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1), zap.Fields(zap.Int("serverID", serverId)))

}

func startServer(id int, t time.Duration) {

	initLogger(id)
	logger = loggerMain.Sugar()

	logger.Debug("Enter")
	defer logger.Debug("Exit")
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
	}

	logger.Infof("Server Initialized successfully with serverId: %d and timer duration: %d", id, t)
}

func main() {

	serverId := 1
	leaderTimeout := 10 * time.Second
	startServer(serverId, leaderTimeout)
	logger.Infof("Server-%d is up and running. Waiting for requests", serverId)
}
