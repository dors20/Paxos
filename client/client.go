package main

import (
	"log"
	"os"
	"os/exec"
	"paxos/constants"
	"paxos/logger"
	"strconv"
	"time"

	"go.uber.org/zap"
)

type serverDetails struct {
	pid int
	cmd *exec.Cmd
}

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

	// TODO parse and run test cases

	logs.Info("Completed all test cases")

}
