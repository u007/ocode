// internal/orchestrator/states.go
package orchestrator

// State is the current step of the pipeline state machine.
type State string

const (
	StatePlanning   State = "Planning"
	StateExploring  State = "Exploring"
	StateDeveloping State = "Developing"
	StateCompiling  State = "Compiling"
	StateValidating State = "Validating"
	StateAdvising   State = "Advising"
	StateDone       State = "Done"
)
