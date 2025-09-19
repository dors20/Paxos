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
		currLeader: 1,
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
	time.Sleep(2 * time.Second)

	logs.Info("Running test Cases")
	run()
	// TODO parse and run test cases
	logs.Info("Completed all test cases")

}

func run() {
	logs.Debug("Enter")
	defer logs.Debug("Exit")

	var wg sync.WaitGroup
	wg.Add(5)
	t := time.Now().UnixNano()
	go sm.makeRequest(&wg, "Alice", "Bob", 10, t)
	go sm.makeRequest(&wg, "Alice", "Bob", 30, time.Now().UnixNano())
	go sm.makeRequest(&wg, "Bob", "Alice", 60, time.Now().UnixNano())
	go sm.makeRequest(&wg, "Alice", "Bob", 25, time.Now().UnixNano())
	// Duplicate request
	go sm.makeRequest(&wg, "Alice", "Bob", 10, t)
	wg.Wait()
	time.Sleep(time.Second)
	printReqStatus(1, 1)
	printServerStatus(1)
	printServerStatus(2)
	printServerStatus(3)
	logs.Info("All requests completed")
}

func (sm *serversData) makeRequest(wg *sync.WaitGroup, sender string, reciever string, amount int, timestamp int64) {
	logs.Debug("Enter")
	defer logs.Debug("Exit")

	logs.Infof("Sending transaction: %s -> %s, Amount: %d at time: %d", sender, reciever, amount, timestamp)

	defer wg.Done()

	conn, err := sm.getClientServerConn()
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*constants.REQUEST_TIMEOUT)
	defer cancel()
	reply, err := conn.Request(ctx, &api.Message{
		Sender:    sender,
		Receiver:  reciever,
		Amount:    int32(amount),
		Timestamp: timestamp,
		ClientId:  sender,
	})
	if err != nil {
		logs.Warnf("Failed Request with error : %v", err)
		// TODO Implement retry to all nodes
	}
	if reply == nil {
		logs.Warnf("Received a nil reply from the server without an error.")
		return
	}
	if reply.GetError() != "" {
		logs.Warnf("Recieved error from server; %s", reply.GetError())
	} else {
		logs.Infof("Recieved response with Success: %t and ballotVal: %d", reply.Result, reply.BallotVal)
	}
	sm.lock.Lock()
	if sm.currLeader != int(reply.ServerId) {
		// TODO Maybe check for range
		sm.currLeader = int(reply.ServerId)
		logs.Infof("Detected new server cluster leader %d, using this server for future requests", sm.currLeader)
	}
	sm.lock.Unlock()
}

// Good to have for performance
// Unlock before creating a new connection because it might take time
// Reaquire lock and then add it to server map
func (sm *serversData) getClientServerConn() (api.ClientServerTxnsClient, error) {
	logs.Debug("Enter")
	defer logs.Debug("Exit")

	sm.lock.Lock()
	defer sm.lock.Unlock()

	server, ok := sm.servers[sm.currLeader]
	if !ok {
		logs.Warnf("No server with id %d found in server map", sm.currLeader)
		return nil, fmt.Errorf("server with ID %d not found", sm.currLeader)
	}

	if server.conn == nil {
		logs.Infof("Creating server %d connection for the first time", sm.currLeader)
		address := fmt.Sprintf("localhost:%s", constants.ServerPorts[sm.currLeader])
		conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			logs.Fatalf("Could not connect to server %d: %v", sm.currLeader, err)
			return nil, fmt.Errorf("Could not connect to server %d: %v", sm.currLeader, err)
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

	logs.Info("------------------------------------")
	logs.Info("------------Server State------------")

	sm.lock.Lock()

	server, ok := sm.servers[serverID]
	if !ok {
		logs.Warnf("No server info for server-%d found", serverID)
		return
	}
	if server.conn == nil {
		err := sm.getConn(serverID)
		if err != nil {
			logs.Warnf("Failed to create conn for server-%d", serverID)
			return
		}
	}
	client := api.NewPaxosPrintInfoClient(server.conn)
	sm.lock.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Print DB
	db, err := client.PrintDB(ctx, &api.Blank{})
	if err != nil {
		logs.Warnf("PrintDB failed: %v", err)
	} else {
		logs.Infof("Server %d Vault: %v", serverID, db.GetVault())
	}

	// Print Log
	logStore, err := client.PrintLog(ctx, &api.Blank{})
	if err != nil {
		logs.Warnf("PrintLog failed: %v", err)
	} else {
		logs.Infof("Server %d Log:", serverID)
		for _, entry := range logStore.GetLogs() {
			logs.Infof("  - Seq: %d, From: %s, To: %s, Amt: %d, Committed: %t, ballotVal: %d, serverID: %d", entry.GetSeqNum(), entry.GetSender(), entry.GetReceiver(), entry.GetAmount(), entry.GetIsCommitted(), entry.GetBallotVal(), entry.ServerId)
		}
	}
	logs.Info("-----------------------------------------")
}

func printReqStatus(serverID, seqNum int) {
	logs.Debug("Enter")
	defer logs.Debug("Exit")

	logs.Info("------------------------------------")
	logs.Info("------------Request State------------")

	sm.lock.Lock()

	server, ok := sm.servers[serverID]
	if !ok {
		logs.Warnf("No server info for server-%d found", serverID)
		return
	}
	if server.conn == nil {
		err := sm.getConn(serverID)
		if err != nil {
			logs.Warnf("Failed to create conn for server-%d", serverID)
			return
		}
	}
	client := api.NewPaxosPrintInfoClient(server.conn)
	sm.lock.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	status, err := client.PrintStatus(ctx, &api.RequestInfo{SeqNum: int32(seqNum)})
	if err != nil {
		logs.Warnf("PrintStatus failed: %v", err)
	} else {
		logs.Infof("Server %d Status for Seq #%d: %s", serverID, seqNum, status.GetStatus().String())
	}
	logs.Info("-----------------------------------------")

}
