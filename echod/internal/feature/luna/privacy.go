package luna

// MicAllowed uses the current mute state, not CutHere (the kind of mute).
// Unknown/unavailable backends and failed privacy reads must never enable capture or wake.
func MicAllowed(m interface{ Muted() (bool, error) }) bool {
	backend, ok := m.(interface{ Available() bool })
	if !ok || !backend.Available() {
		return false
	}
	muted, err := m.Muted()
	return err == nil && !muted
}
