package herdr

import (
	"context"
	"fmt"

	"github.com/kriuchkov/herdr-scope/internal/domain"
)

// Observer reads agents and sessions; it implements ports.AgentSource,
// ports.SessionSource and ports.ServerProbe.
type Observer struct {
	client *Client
	mapper Mapper
}

// NewObserver binds a mapper to a client.
func NewObserver(client *Client, mapper Mapper) *Observer {
	return &Observer{client: client, mapper: mapper}
}

// ListAgents is agent.list. A failure is an error, never an empty list: to a
// caller tracking liveness those are opposite claims.
func (o *Observer) ListAgents(ctx context.Context) ([]domain.Agent, error) {
	var list wireAgentList
	if err := o.client.call(ctx, "agent.list", struct{}{}, &list); err != nil {
		return nil, err
	}
	return o.mapper.Agents(list.Agents), nil
}

// Snapshot is session.snapshot.
func (o *Observer) Snapshot(ctx context.Context) (domain.Session, error) {
	var session wireSession
	if err := o.client.call(ctx, "session.snapshot", struct{}{}, &session); err != nil {
		return domain.Session{}, err
	}
	if session.Snapshot.Protocol == 0 {
		return domain.Session{}, fmt.Errorf("%w: session.snapshot without a protocol", domain.ErrDecode)
	}
	return o.mapper.Session(session.Snapshot), nil
}

// Ping identifies the server.
func (o *Observer) Ping(ctx context.Context) (domain.ServerInfo, error) {
	return o.client.Ping(ctx)
}

// Gone reports whether the server's endpoint disappeared.
func (o *Observer) Gone() bool {
	return o.client.Gone()
}
