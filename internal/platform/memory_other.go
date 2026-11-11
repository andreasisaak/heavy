//go:build !darwin

// Package platform names what differs between the systems heavy runs on.
package platform

// Docker is where docker runs its containers, as heavy names it to the user: on Linux the machine itself.
const Docker = "machine"
