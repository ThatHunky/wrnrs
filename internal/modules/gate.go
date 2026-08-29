package modules

// Reason keys are i18n string keys, resolved by the caller.
const (
	ReasonNeeds18Plus  = "gate.needs_18plus"
	ReasonNeedsMature  = "gate.needs_mature"
	ReasonNeedsPair    = "gate.needs_pair"
	ReasonNeedsPremium = "gate.needs_premium"
)

// Gate declares what a module requires before it can be opened.
type Gate struct {
	NeedsPair    bool
	Needs18Plus  bool
	NeedsMature  bool
	NeedsPremium bool
}

// UserState is the resolved access state of one user.
type UserState struct {
	Is18Plus      bool
	MatureOptIn   bool
	HasActivePair bool
	HasPremium    bool
}

// Allows reports whether the user may open the module. When blocked it returns
// the i18n key of the first unmet requirement, checked in a fixed order so the
// message is the most useful one.
func (g Gate) Allows(s UserState) (bool, string) {
	if g.Needs18Plus && !s.Is18Plus {
		return false, ReasonNeeds18Plus
	}
	if g.NeedsMature && !s.MatureOptIn {
		return false, ReasonNeedsMature
	}
	if g.NeedsPair && !s.HasActivePair {
		return false, ReasonNeedsPair
	}
	if g.NeedsPremium && !s.HasPremium {
		return false, ReasonNeedsPremium
	}
	return true, ""
}
