package modules

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"wrnrs/internal/telegram"
)

// Handler owns every update whose callback data starts with the module prefix.
type Handler interface {
	HandleCallback(ctx context.Context, cb *telegram.CallbackQuery) error
	// HandleMessage reports whether it consumed the message. False means the
	// caller keeps looking for another owner.
	HandleMessage(ctx context.Context, msg *telegram.Message) (bool, error)
}

// Module is one feature of the superapp: its menu entry, its access gate and
// the handler that owns its callbacks.
type Module struct {
	ID             string
	TitleKey       string
	Icon           string
	CallbackPrefix string
	Gate           Gate
	Handler        Handler
}

type Registry struct {
	modules []Module
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Register(m Module) error {
	if strings.TrimSpace(m.ID) == "" {
		return errors.New("module id must not be empty")
	}
	if !strings.HasSuffix(m.CallbackPrefix, ":") {
		return fmt.Errorf("module %s callback prefix %q must end with ':'", m.ID, m.CallbackPrefix)
	}
	for _, existing := range r.modules {
		if existing.ID == m.ID {
			return fmt.Errorf("module %s is already registered", m.ID)
		}
		if strings.HasPrefix(existing.CallbackPrefix, m.CallbackPrefix) ||
			strings.HasPrefix(m.CallbackPrefix, existing.CallbackPrefix) {
			return fmt.Errorf("module %s prefix %q collides with module %s prefix %q",
				m.ID, m.CallbackPrefix, existing.ID, existing.CallbackPrefix)
		}
	}
	r.modules = append(r.modules, m)
	return nil
}

func (r *Registry) All() []Module {
	out := make([]Module, len(r.modules))
	copy(out, r.modules)
	return out
}

func (r *Registry) ByCallback(data string) (Module, bool) {
	for _, m := range r.modules {
		if strings.HasPrefix(data, m.CallbackPrefix) {
			return m, true
		}
	}
	return Module{}, false
}
