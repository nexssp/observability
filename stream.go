package obs

import "time"

type StreamStats struct {
	Stream StreamIdentity  `json:"stream"`
	Exec   StreamExecution `json:"execution"`
	Items  StreamItems     `json:"items"`
	Bytes  StreamBytes     `json:"bytes,omitzero"`
	Error  *StreamError    `json:"error,omitempty"`
}

type StreamIdentity struct {
	Name      string `json:"name"`
	RequestID string `json:"request_id,omitempty"`
	TraceID   string `json:"trace_id,omitempty"`
}

type StreamExecution struct {
	StartedAt time.Time     `json:"started_at"`
	Duration  time.Duration `json:"duration_ns"`
	Status    StreamStatus  `json:"status"`
	Canceled  bool          `json:"canceled,omitempty"`
	Truncated bool          `json:"truncated,omitempty"`
}

type StreamStatus string

const (
	StreamStatusCompleted StreamStatus = "completed"
	StreamStatusFailed    StreamStatus = "failed"
	StreamStatusCanceled  StreamStatus = "canceled"
	StreamStatusTimeout   StreamStatus = "timeout"
	StreamStatusStopped   StreamStatus = "stopped"
)

type StreamItems struct {
	Emitted   int64 `json:"emitted"`
	Delivered int64 `json:"delivered"`
	Dropped   int64 `json:"dropped,omitempty"`
	Errors    int64 `json:"errors,omitempty"`
}

type StreamBytes struct {
	Emitted   int64 `json:"emitted,omitempty"`
	Delivered int64 `json:"delivered,omitempty"`
}

type StreamError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}
