package herdr

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/domain"
)

// envelope accepts either a whole response line or just its result.
type envelope struct {
	Result json.RawMessage `json:"result"`
}

func resultOf(raw []byte) json.RawMessage {
	var env envelope
	if json.Unmarshal(raw, &env) == nil && len(env.Result) > 0 {
		return env.Result
	}
	return raw
}

// DecodeSnapshot decodes a session.snapshot response (or its result) into a
// domain session — for contract tests and offline tools.
func DecodeSnapshot(m Mapper, raw []byte) (domain.Session, error) {
	var session wireSession
	if err := json.Unmarshal(resultOf(raw), &session); err != nil {
		return domain.Session{}, fmt.Errorf("%w: session.snapshot: %w", domain.ErrDecode, err)
	}
	return m.Session(session.Snapshot), nil
}

// DecodeAgents decodes an agent.list response (or its result).
func DecodeAgents(m Mapper, raw []byte) ([]domain.Agent, error) {
	var list wireAgentList
	if err := json.Unmarshal(resultOf(raw), &list); err != nil {
		return nil, fmt.Errorf("%w: agent.list: %w", domain.ErrDecode, err)
	}
	return m.Agents(list.Agents), nil
}

// DecodeEvent decodes one event line.
func DecodeEvent(raw []byte) (domain.AgentEvent, error) {
	var frame struct {
		Event string          `json:"event"`
		Data  json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		return domain.AgentEvent{}, fmt.Errorf("%w: event: %w", domain.ErrDecode, err)
	}
	return Normalize(frame.Event, frame.Data, time.Time{}), nil
}
