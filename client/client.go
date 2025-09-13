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
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type serverDetails struct {
	pid  int
	cmd  *exec.Cmd
	conn api.ClientServerTxnsClient
}

var logs *zap.SugaredLogger
var port string

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

	serverInfo := make(map[int]*serverDetails)

	for i := 1; i <= constants.MAX_NODES; i++ {

		launchCmd := exec.Command("./server_bin", strconv.Itoa(i))
		err = launchCmd.Start()
		if err != nil {
			logs.Fatalf("Failed to start server-%d process: %v", i, err)
		}

		serverPid := launchCmd.Process.Pid
		serverInfo[i] = &serverDetails{pid: serverPid, cmd: launchCmd}
		logs.Infof("Server-%d started successfully with PID: %d", i, serverPid)
	}

	port = constants.BASE_PORT
	logs.Infof("Client communications on port %s", port)

	defer func() {

		logs.Info("No test cases to execute. Shutting down all servers")

		for i := 1; i <= constants.MAX_NODES; i++ {
			err = serverInfo[i].cmd.Process.Kill()
			if err != nil {
				logs.Warnf("Failed to stop server-%d with PID: %d due to err: %v", i, serverInfo[i].pid, err)
			} else {
				logs.Warnf("Successfully shut down server-%d with PID: %d", i, serverInfo[i].pid)
			}
		}
		os.Remove("server_bin")
	}()

	logs.Info("Waiting for servers to start")
	time.Sleep(10 * time.Second)

	logs.Info("Running test Cases")
	run()
	// TODO parse and run test cases
	// close all connections
	logs.Info("Completed all test cases")

}

func run() {
	logs.Debug("Enter")
	defer logs.Debug("Exit")

	// Todo store already created connections
	address := fmt.Sprintf("localhost:%s", constants.ServerPorts[1])
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logs.Fatalf("Test Case 1 Failed: Could not connect to server: %v", err)
	}
	defer conn.Close()

	client := api.NewClientServerTxnsClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	logs.Info("Sending transaction: Alice -> Bob, Amount: 10")
	reply, err := client.Request(ctx, &api.Message{
		Sender:    "Alice",
		Receiver:  "Bob",
		Amount:    10,
		Timestamp: time.Now().Unix(),
		ClientId:  "Alice",
	})
	if err != nil {
		logs.Warnf("Failed Request with error : %v", err)
		return
		// TODO Implement retry to all nodes

	}
	logs.Infof("Recieved response with Success: %t and ballotVal: %d", reply.Result, reply.BallotVal)

}
