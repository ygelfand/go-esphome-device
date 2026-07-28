package esphomedevice

import (
	"context"
	"fmt"
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/go-esphome-device/api"
)

// Entities holds the device's entity set and answers the messages that operate on it:
// ListEntities, the initial state dump after SubscribeStates, and the per-domain
// command requests. Use it as a Server's Handler.
//
// Wrap it with Chain to combine with other handlers, such as a voice satellite.
type Entities struct {
	mu        sync.RWMutex
	ordered   []Entity
	byKey     map[uint32]Entity
	broadcast func(proto.Message)
}

func NewEntities() *Entities {
	return &Entities{byKey: map[uint32]Entity{}}
}

// Add registers an entity. It panics on a duplicate key, which means two entities share
// an ObjectID — always a bug, and one that silently breaks state routing.
func (e *Entities) Add(ents ...Entity) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, ent := range ents {
		key := ent.Key()
		if existing, dup := e.byKey[key]; dup {
			panic(fmt.Sprintf("esphomedevice: duplicate entity key %d (%T and %T share an ObjectID)", key, existing, ent))
		}
		e.byKey[key] = ent
		e.ordered = append(e.ordered, ent)

		if n, ok := ent.(interface{ setNotifier(func(proto.Message)) }); ok {
			n.setNotifier(e.push)
		}
	}
}

// bindServer routes entity state changes to the server's subscribers. Serve calls this
// automatically, so forgetting it cannot silently swallow state updates.
func (e *Entities) bindServer(s *Server) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.broadcast = func(msg proto.Message) { _ = s.Broadcast(msg) }
}

func (e *Entities) push(msg proto.Message) {
	e.mu.RLock()
	fn := e.broadcast
	e.mu.RUnlock()
	if fn != nil {
		fn(msg)
	}
}

func (e *Entities) lookup(key uint32) (Entity, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	ent, ok := e.byKey[key]
	return ent, ok
}

func (e *Entities) all() []Entity {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Entity, len(e.ordered))
	copy(out, e.ordered)
	return out
}

func (e *Entities) Handle(ctx context.Context, c *Conn, msg proto.Message) error {
	switch m := msg.(type) {
	case *api.ListEntitiesRequest:
		for _, ent := range e.all() {
			if err := c.Send(ent.describe()); err != nil {
				return err
			}
		}
		return c.Send(&api.ListEntitiesDoneResponse{})

	case *api.SubscribeStatesRequest:
		for _, ent := range e.all() {
			// Stateless domains such as Button return nil.
			st := ent.state()
			if st == nil {
				continue
			}
			if err := c.Send(st); err != nil {
				return err
			}
		}
		return nil

	case *api.SelectCommandRequest:
		return dispatchCommand(e, m.GetKey(), func(s *Select) { s.command(m.GetState()) })

	case *api.SwitchCommandRequest:
		return dispatchCommand(e, m.GetKey(), func(s *Switch) { s.command(m.GetState()) })

	case *api.NumberCommandRequest:
		return dispatchCommand(e, m.GetKey(), func(n *Number) { n.command(m.GetState()) })

	case *api.ButtonCommandRequest:
		return dispatchCommand(e, m.GetKey(), func(b *Button) { b.command() })

	case *api.LightCommandRequest:
		return dispatchCommand(e, m.GetKey(), func(l *Light) { l.command(m) })

	case *api.MediaPlayerCommandRequest:
		return dispatchCommand(e, m.GetKey(), func(p *MediaPlayer) { p.command(m) })

	case *api.UpdateCommandRequest:
		return dispatchCommand(e, m.GetKey(), func(u *Update) { u.command(m) })
	}
	return nil
}

func dispatchCommand[T Entity](e *Entities, key uint32, fn func(T)) error {
	ent, ok := e.lookup(key)
	if !ok {
		return nil
	}
	typed, ok := ent.(T)
	if !ok {
		return fmt.Errorf("esphomedevice: entity %d is %T, not %T", key, ent, *new(T))
	}
	fn(typed)
	return nil
}

// Chain runs handlers in order, stopping at the first error.
//
// It passes bindServer through to whichever handlers want it. Without that, chaining Entities with
// anything else silently loses every state update: the server binds its handler by type, a plain
// func is not bindable, and only the states sent on connect would ever reach Home Assistant.
func Chain(handlers ...Handler) Handler {
	return &chain{handlers: handlers}
}

type chain struct{ handlers []Handler }

func (c *chain) Handle(ctx context.Context, conn *Conn, msg proto.Message) error {
	for _, h := range c.handlers {
		if h == nil {
			continue
		}
		if err := h.Handle(ctx, conn, msg); err != nil {
			return err
		}
	}
	return nil
}

func (c *chain) bindServer(s *Server) {
	for _, h := range c.handlers {
		if b, ok := h.(serverBinder); ok {
			b.bindServer(s)
		}
	}
}
