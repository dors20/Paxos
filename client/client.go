package main

import (
	"log"
	"os"
	"os/exec"
	"paxos/logger"
	"time"

	"go.uber.org/zap"
)

var logs *zap.SugaredLogger

// TODO Launch servers in for loop and pass server with ids for them to set

func main() {
	logs = logger.InitLogger(1, false)
	logs.Info("Client Started")
	defer logs.Info("Stopping Client")

	cmd := exec.Command("go", "build", "-o", "server_bin", "../server/")
	err := cmd.Run()
	if err != nil {
		log.Fatalf("Failed to build server: %v", err)
	}
	logs.Info("Server Binary built")

	launchCmd := exec.Command("./server_bin")
	err = launchCmd.Start()
	if err != nil {
		logs.Fatalf("Failed to start server process: %v", err)
	}

	serverPid := launchCmd.Process.Pid
	logs.Infof("Server started successfully with PID: %d", serverPid)

	defer func() {
		logs.Info("No test cases to execute. Shutting down all servers")
		err = launchCmd.Process.Kill()
		if err != nil {
			logs.Warnf("Failed to kill server with PID: %d due to err: %v", serverPid, err)
		} else {
			logs.Warnf("Successfully shut down server with PID: %d", serverPid)
		}
		os.Remove("server_bin")
	}()

	logs.Info("Waiting for servers to start")
	time.Sleep(2 * time.Second)

	logs.Info("Running test Cases")

	// TODO parse and run test cases

	logs.Info("Completed all test cases")

}
