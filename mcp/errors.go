package mcp

type TransportError struct {
	Message string
	Cause   error
}

func (e TransportError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e TransportError) Unwrap() error {
	return e.Cause
}

type StartupError struct{ TransportError }
type TimeoutError struct{ TransportError }
