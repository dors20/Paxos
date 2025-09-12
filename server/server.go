package main

import (
	"sync"
	"time"
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
