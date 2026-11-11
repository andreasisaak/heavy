package client_test

// unmeasured is the line of --status about the Docker VM: the daemon of a test binary, which has no build stamp,
// starts no helper container. The tests that expect it put a docker on PATH, since a machine without docker, such
// as the macOS runner of the CI, names the missing docker instead.
const unmeasured = "VM: not measured (no build stamp in bin/.stamp)\n"
