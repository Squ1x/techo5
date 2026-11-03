package config

import (
	"encoding/json"
	"errors"
)

// Luna selects the native terminal backend when URL and Token are both nonempty.
// Change through the owner-only JSON settings file and restart echod.
type Luna struct {
	URL            string            `json:"url,omitempty"`
	Token          string            `json:"token,omitempty"`
	WakeAssistants map[string]string `json:"wake_assistants,omitempty"`
}

func (l Luna) Enabled() bool { return l.URL != "" && l.Token != "" }

// Decode atomically: an invalid mapping must not leave an enabled partial backend.
func (l *Luna) UnmarshalJSON(data []byte) error {
	*l = Luna{}
	type plain Luna
	var next plain
	if err := json.Unmarshal(data, &next); err != nil {
		return err
	}
	for model, assistant := range next.WakeAssistants {
		if model == "" || (assistant != "Luna" && assistant != "Astra") {
			return errors.New("invalid Luna wake assistant mapping")
		}
	}
	*l = Luna(next)
	return nil
}
