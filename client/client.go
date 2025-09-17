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

// TODO Launch servers in for loop and pass server with ids for them to set

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

	// Todo store already created connections

	logs.Info("Sending transaction: Alice -> Bob, Amount: 10")

	var wg sync.WaitGroup
	wg.Add(3)
	go sm.makeRequest(&wg, "Alice", "Bob", 10, time.Now().Unix())
	go sm.makeRequest(&wg, "Alice", "Bob", 30, time.Now().Unix())
	go sm.makeRequest(&wg, "Bob", "Alice", 40, time.Now().Unix())

	wg.Wait()
	logs.Info("All requests completed")
}

func (sm *serversData) makeRequest(wg *sync.WaitGroup, sender string, reciever string, amount int, timestamp int64) {
	logs.Debug("Enter")
	defer logs.Debug("Exit")

	defer wg.Done()

	conn, err := sm.getClientServerConn()
	if err != nil {
		return
	}
	// TODO Use constant.go request timeout
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
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
	logs.Infof("Recieved response with Success: %t and ballotVal: %d", reply.Result, reply.BallotVal)
	sm.lock.Lock()
	if sm.currLeader != int(reply.ServerId) {
		// TODO Maybe check for range
		sm.currLeader = int(reply.ServerId)
		logs.Infof("Detected new server cluster leader %d, using this server for future requests", sm.currLeader)
	}
	sm.lock.Unlock()
}

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
