package constants

const (
	Follower = iota
	Candidate
	Leader
	Failed
)

// Define other constants like timeouts here
// TODO

// SYSTEM CONFIG
const MAX_CLIENTS = 10
const MAX_NODES = 3

// NETWORK CONFIG
const LEADER_TIMEOUT_SECONDS = 10
const BASE_PORT = 9100

// Can do base_Port+1
// Port 9101 - 9110 reserved if we need multiple client instances
// Static ports and IP based on the assumption syaing all nodes are aware of all other clients and nodes
var ServerPorts = map[int]int{
	1: 9111,
	2: 9112,
	3: 9113,
	4: 9114,
	5: 9115,
}
