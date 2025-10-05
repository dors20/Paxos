package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"paxos/api"
	"paxos/constants"
	"paxos/logger"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type serverInfo struct {
	pid  int
	cmd  *exec.Cmd
	conn *grpc.ClientConn
}

type serversData struct {
	lock       sync.Mutex
	servers    map[int]*serverInfo
	currLeader int
}

var logs *zap.SugaredLogger
var port string
var sm *serversData

func main() {
	logs = logger.InitLogger(1, false)
	logs.Debug("Client Started")
	defer logs.Debug("Stopping Client")

	cmd := exec.Command("go", "build", "-o", "server_bin", "../server/")
	err := cmd.Run()
	if err != nil {
		log.Fatalf("Failed to build server: %v", err)
	}
	logs.Info("Server Binary built")

	sm = &serversData{
		servers:    make(map[int]*serverInfo),
		currLeader: constants.MAX_NODES,
	}

	for i := 1; i <= constants.MAX_NODES; i++ {

		launchCmd := exec.Command("./server_bin", strconv.Itoa(i))
		err = launchCmd.Start()
		if err != nil {
			logs.Fatalf("Failed to start server-%d process: %v", i, err)
		}

		serverPid := launchCmd.Process.Pid
		sm.servers[i] = &serverInfo{pid: serverPid, cmd: launchCmd}
		logs.Infof("Server-%d started successfully with PID: %d", i, serverPid)
	}

	port = constants.BASE_PORT
	logs.Infof("Client communications on port %s", port)

	defer func() {

		logs.Info("No test cases to execute. Shutting down all servers")

		for i := 1; i <= constants.MAX_NODES; i++ {
			if server, ok := sm.servers[i]; ok {
				if server.conn != nil {
					server.conn.Close()
				}
				err = server.cmd.Process.Kill()
				if err != nil {
					logs.Warnf("Failed to stop server-%d with PID: %d due to err: %v", i, sm.servers[i].pid, err)
				} else {
					logs.Warnf("Successfully shut down server-%d with PID: %d", i, sm.servers[i].pid)
				}
			}
		}
		os.Remove("server_bin")
	}()

	logs.Info("Waiting for servers to start")
	time.Sleep(15 * time.Second)

	logs.Info("Running test Cases")
	run()
	// TODO parse and run test cases
	logs.Info("Completed all test cases")

}

func run() {
	logs.Debug("Enter")
	defer logs.Debug("Exit")

	var wg sync.WaitGroup
	wg.Add(7)
	t := time.Now().UnixNano()
	go sm.makeRequest(&wg, "Alice", "Bob", 10, t)
	go sm.makeRequest(&wg, "Alice", "Bob", 30, time.Now().UnixNano())
	go sm.makeRequest(&wg, "Bob", "Alice", 60, time.Now().UnixNano())
	go sm.makeRequest(&wg, "Alice", "Bob", 25, time.Now().UnixNano())
	go sm.makeRequest(&wg, "A", "B", 30, time.Now().UnixNano())
	go sm.makeRequest(&wg, "B", "A", 60, time.Now().UnixNano())
	go sm.makeRequest(&wg, "A", "B", 25, time.Now().UnixNano())
	wg.Wait()
	// Duplicate request
	time.Sleep(20 * time.Second)
	var wg1 sync.WaitGroup
	wg1.Add(1)
	go sm.makeRequest(&wg1, "Alice", "Bob", 10, time.Now().UnixNano())
	wg1.Wait()
	time.Sleep(20 * time.Second)
	var wg2 sync.WaitGroup
	wg2.Add(3)
	go sm.makeRequest(&wg2, "Mark", "jack", 30, time.Now().UnixNano())
	go sm.makeRequest(&wg2, "jack", "Alice", 60, time.Now().UnixNano())
	go sm.makeRequest(&wg2, "jack", "Bob", 25, time.Now().UnixNano())
	wg2.Wait()
	time.Sleep(30 * time.Second)
	var wg3 sync.WaitGroup
	wg3.Add(2)
	go sm.makeRequest(&wg3, "jack", "jack", 60, time.Now().UnixNano())
	go sm.makeRequest(&wg3, "zack", "cat", 25, time.Now().UnixNano())
	wg3.Wait()
	time.Sleep(5 * time.Second)
	printReqStatus(1, 1)
	printServerStatus(1)
	printServerStatus(2)
	printServerStatus(3)
	printView(1)
	printView(2)
	printView(3)
	logs.Info("All requests completed")
}

func (sm *serversData) makeRequest(wg *sync.WaitGroup, sender string, reciever string, amount int, timestamp int64) {
	logs.Debug("Enter")
	defer logs.Debug("Exit")
	defer wg.Done()

	logs.Infof("Sending transaction: %s -> %s, Amount: %d at time: %d", sender, reciever, amount, timestamp)

	msg := &api.Message{
		Sender:    sender,
		Receiver:  reciever,
		Amount:    int32(amount),
		Timestamp: timestamp,
		ClientId:  sender,
	}

	conn, err := sm.getClientServerConn()
	var reply *api.Reply

	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*constants.REQUEST_TIMEOUT)
		defer cancel()
		reply, err = conn.Request(ctx, msg)
	}
	if err != nil || (reply != nil && reply.GetError() == "NOT_LEADER") {
		logs.Warnf("Initial request failed or was sent to a non-leader. Broadcasting to all nodes. Error: %v", err)
		reply = sm.broadcastRequest(msg)
	}
	if reply == nil {
		logs.Warnf("Received a nil reply from the server after all attempts.")
		return
	}
	if reply.GetError() != "" {
		logs.Warnf("Recieved error from server; %s", reply.GetError())
		return
	} else {
		logs.Infof("Recieved response with Success: %t and ballotVal: %d from server %d", reply.Result, reply.BallotVal, reply.ServerId)
	}

	sm.lock.Lock()
	if sm.currLeader != int(reply.ServerId) && reply.Result {
		// TODO Maybe check for range
		sm.currLeader = int(reply.ServerId)
		logs.Infof("Detected new server cluster leader %d, using this server for future requests", sm.currLeader)
	}
	sm.lock.Unlock()
}

func (sm *serversData) broadcastRequest(msg *api.Message) *api.Reply {
	logs.Debug("Enter")
	defer logs.Debug("Exit")

	replyChan := make(chan *api.Reply, constants.MAX_NODES)
	var wg sync.WaitGroup

	for i := 1; i <= constants.MAX_NODES; i++ {
		wg.Add(1)
		go func(serverID int) {
			defer wg.Done()
			client, err := sm.getServerClient(serverID)
			if err != nil {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), constants.REQUEST_TIMEOUT*time.Second)
			defer cancel()
			reply, err := client.Request(ctx, msg)
			if err == nil && reply.GetError() == "" {
				select {
				//Todo for performance we can call ctx cancel for the other requests if needed
				case replyChan <- reply:
				default:
				}
			}
		}(i)
	}

	wg.Wait()
	close(replyChan)

	if firstReply, ok := <-replyChan; ok {
		return firstReply
	}

	return nil
}

// Good to have for performance
// Unlock before creating a new connection because it might take time
// Reaquire lock and then add it to server map
func (sm *serversData) getClientServerConn() (api.ClientServerTxnsClient, error) {
	logs.Debug("Enter")
	defer logs.Debug("Exit")
	sm.lock.Lock()
	serverID := sm.currLeader
	sm.lock.Unlock()
	return sm.getServerClient(serverID)
}

func (sm *serversData) getServerClient(serverId int) (api.ClientServerTxnsClient, error) {
	logs.Debug("Enter")
	defer logs.Debug("Exit")

	sm.lock.Lock()
	defer sm.lock.Unlock()

	server, ok := sm.servers[serverId]
	if !ok {
		logs.Warnf("No server with id %d found in server map", serverId)
		return nil, fmt.Errorf("server with ID %d not found", serverId)
	}

	if server.conn == nil {
		logs.Infof("Creating server %d connection for the first time", serverId)
		address := fmt.Sprintf("localhost:%s", constants.ServerPorts[serverId])
		conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			logs.Fatalf("Could not connect to server %d: %v", serverId, err)
			return nil, fmt.Errorf("Could not connect to server %d: %v", serverId, err)
		}
		server.conn = conn
	}

	client := api.NewClientServerTxnsClient(server.conn)

	return client, nil
}

func (sm *serversData) getConn(serverId int) error {
	logs.Debug("Enter")
	defer logs.Debug("Exit")

	server, ok := sm.servers[serverId]
	if !ok {
		logs.Warnf("No server with id %d found in server map", serverId)
		return fmt.Errorf("server with ID %d not found", serverId)
	}

	if server.conn == nil {
		logs.Infof("Creating server %d connection for the first time", serverId)
		// TODO Safety check for bound
		address := fmt.Sprintf("localhost:%s", constants.ServerPorts[serverId])
		conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			logs.Fatalf("Could not connect to server %d: %v", serverId, err)
			return fmt.Errorf("Could not connect to server %d: %v", serverId, err)
		}
		server.conn = conn
	}
	return nil
}

func printServerStatus(serverID int) {
	logs.Debug("Enter")
	defer logs.Debug("Exit")

	// Print DB
	printDb(serverID)
	// Print Log
	printlog(serverID)

}

func printReqStatus(serverID, seqNum int) {
	logs.Debug("Enter")
	defer logs.Debug("Exit")

	client, err := getPrintClient(serverID)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), constants.REQUEST_TIMEOUT*time.Second)
	defer cancel()

	status, err := client.PrintStatus(ctx, &api.RequestInfo{SeqNum: int32(seqNum)})
	if err != nil {
		logs.Warnf("PrintStatus failed: %v", err)
	} else {
		logs.Infof("*PRINTSTATUS* Server %d Status for Seq #%d: %s", serverID, seqNum, status.GetStatus().String())
	}

}

func printDb(serverID int) {
	logs.Debug("Enter")
	defer logs.Debug("Exit")

	client, err := getPrintClient(serverID)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), constants.REQUEST_TIMEOUT*time.Second)
	defer cancel()

	db, err := client.PrintDB(ctx, &api.Blank{})
	if err != nil {
		logs.Warnf("PrintDB failed: %v", err)
	} else {
		logs.Infof("*PRINT DB* Server %d Vault: %v", serverID, db.GetVault())
	}
}

func printlog(serverID int) {
	logs.Debug("Enter")
	defer logs.Debug("Exit")

	client, err := getPrintClient(serverID)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), constants.REQUEST_TIMEOUT*time.Second)
	defer cancel()

	logStore, err := client.PrintLog(ctx, &api.Blank{})
	if err != nil {
		logs.Warnf("PrintLog failed: %v", err)
	} else {
		logs.Infof("Server %d Log:", serverID)
		for _, entry := range logStore.GetLogs() {
			logs.Infof("*PRINT LOGS* - Seq: %d, From: %s, To: %s, Amt: %d, Committed: %t, ballotVal: %d, serverID: %d", entry.GetSeqNum(), entry.GetSender(), entry.GetReceiver(), entry.GetAmount(), entry.GetIsCommitted(), entry.GetBallotVal(), entry.ServerId)
		}
	}
}

func printView(serverID int) {
	logs.Debug("Enter")
	defer logs.Debug("Exit")

	client, err := getPrintClient(serverID)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), constants.REQUEST_TIMEOUT*time.Second)
	defer cancel()

	viewHistory, err := client.PrintView(ctx, &api.Blank{})
	if err != nil {
		logs.Warnf("PrintView failed: %v", err)
	} else {
		logs.Infof("Server %d View History:", serverID)
		for i, view := range viewHistory.GetViews() {
			logs.Infof("*PRINT VIEW* - View %d: Leader <%d, %d>", i+1, view.GetBallotVal(), view.GetServerId())
			for _, entry := range view.GetAcceptLog() {
				logs.Infof("    - Seq: %d, From: %s, To: %s, Amt: %d", entry.GetSeqNum(), entry.GetSender(), entry.GetReceiver(), entry.GetAmount())
			}
		}
	}
}

func getPrintClient(serverID int) (api.PaxosPrintInfoClient, error) {
	sm.lock.Lock()
	defer sm.lock.Unlock()

	server, ok := sm.servers[serverID]
	if !ok {
		logs.Warnf("No server info for server-%d found", serverID)
		return nil, fmt.Errorf("no server Info found")
	}
	if server.conn == nil {
		err := sm.getConn(serverID)
		if err != nil {
			logs.Warnf("Failed to create conn for server-%d", serverID)
			return nil, fmt.Errorf("failed to create client")
		}
	}
	client := api.NewPaxosPrintInfoClient(server.conn)

	return client, nil
}
