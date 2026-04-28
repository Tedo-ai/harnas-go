package harnas

import (
	"crypto/rand"
	"fmt"
)

type Session struct {
	ID       string
	Log      *Log
	Metadata map[string]any
	Hooks    *Hooks
}

func NewSession(id string, log *Log, metadata map[string]any) *Session {
	if log == nil {
		log = NewLog()
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	return &Session{
		ID:       id,
		Log:      log,
		Metadata: metadata,
		Hooks:    NewHooks(),
	}
}

func CreateSession(metadata map[string]any) *Session {
	return NewSession("ses_"+newID(), NewLog(), metadata)
}

func (s *Session) Fork(atSeq int) *Session {
	forkedLog := NewLog()
	for _, event := range s.Log.Events() {
		if event.Seq > atSeq {
			break
		}
		forkedLog.Restore(event)
	}
	metadata := map[string]any{}
	for key, value := range s.Metadata {
		metadata[key] = value
	}
	metadata["forked_from"] = s.ID
	metadata["forked_at_seq"] = float64(atSeq)
	return NewSession("ses_"+newID(), forkedLog, metadata)
}

func newID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(err)
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		bytes[0:4],
		bytes[4:6],
		bytes[6:8],
		bytes[8:10],
		bytes[10:16],
	)
}
