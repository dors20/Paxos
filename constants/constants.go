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
