package cli

// ForkRepo is the GitHub repository this fork publishes releases to. CLI
// self-update, daemon auto-update and server-pushed updates all read it, so
// none of them can install an upstream build over the fork.
const ForkRepo = "zzzpyyy/multica"
