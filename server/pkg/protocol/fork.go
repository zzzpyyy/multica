package protocol

// DaemonCapabilityForkV1 is advertised by the fork's daemon (zzzpyyy/multica).
// It understands instance rows (claim field `instance`) and the super-agent's
// `no_tools` / `server_prompt`. The server refuses those tasks to a daemon
// without it, so an upstream daemon never runs a whole issue for one instance
// or starts the super-agent with tools.
const DaemonCapabilityForkV1 = "multica-fork-v1"
