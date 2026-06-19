// internal/orchestrator/states.go
package orchestrator

// state is the current step of the pipeline state machine.
type state string

const (
	statePlanning   state = "Planning"
	stateExploring  state = "Exploring"
	stateDeveloping state = "Developing"
	stateCompiling  state = "Compiling"
	stateValidating state = "Validating"
	stateAdvising   state = "Advising"
	stateDone       state = "Done"
)
