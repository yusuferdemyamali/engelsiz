// Package providers defines the lifecycle boundary used by the application.
package providers

import "context"

type RuntimeState string

const (
	Stopped  RuntimeState = "stopped"
	Starting RuntimeState = "starting"
	Ready    RuntimeState = "ready"
	Stopping RuntimeState = "stopping"
	Failed   RuntimeState = "failed"
)

// RuntimeStatus is an internal value snapshot. It deliberately has no endpoint
// or content paths; public HTTP responses do not depend on runtime details.
type RuntimeStatus struct {
	State     RuntimeState
	PID       int
	LastError string
}

type ContentProvider interface {
	EnsureStarted(context.Context) error
	Stop(context.Context) error
	Restart(context.Context) error
	Status() RuntimeStatus
}
