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

// reservedCallbackPrefixes are the callback-data prefixes already owned by
// the legacy switch in (*App).handleCallback (internal/app/app.go). Because
// dispatchModuleCallback runs before that switch, a module claiming one of
// these would silently and completely shadow the live screen it serves.
var reservedCallbackPrefixes = []string{
	"menu:",
	"pair:",
	"game:",
	"settings:",
	"theme:",
	"onboarding:",
	"admin:",
	"store:",
	"journal:",
	"custom_questions:",
}

func (r *Registry) Register(m Module) error {
	id := strings.TrimSpace(m.ID)
	if id == "" {
		return errors.New("module id must not be empty")
	}
	if strings.TrimSpace(m.TitleKey) == "" {
		return fmt.Errorf("module %s title key must not be empty", id)
	}
	if !strings.HasSuffix(m.CallbackPrefix, ":") {
		return fmt.Errorf("module %s callback prefix %q must end with ':'", id, m.CallbackPrefix)
	}
	for _, reserved := range reservedCallbackPrefixes {
		if strings.HasPrefix(reserved, m.CallbackPrefix) || strings.HasPrefix(m.CallbackPrefix, reserved) {
			return fmt.Errorf("module %s prefix %q collides with reserved prefix %q", id, m.CallbackPrefix, reserved)
		}
	}
	for _, existing := range r.modules {
		if existing.ID == id {
			return fmt.Errorf("module %s is already registered", id)
		}
		if strings.HasPrefix(existing.CallbackPrefix, m.CallbackPrefix) ||
			strings.HasPrefix(m.CallbackPrefix, existing.CallbackPrefix) {
			return fmt.Errorf("module %s prefix %q collides with module %s prefix %q",
				id, m.CallbackPrefix, existing.ID, existing.CallbackPrefix)
		}
	}
	m.ID = id
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

// ByID looks up a registered module by its (trimmed) id. Deep links,
// per-module admin toggles and metrics all need to resolve a module without
// going through a callback prefix.
func (r *Registry) ByID(id string) (Module, bool) {
	for _, m := range r.modules {
		if m.ID == id {
			return m, true
		}
	}
	return Module{}, false
}
