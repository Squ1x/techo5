package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLunaDisabledAndSecuredAfterPartialParseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	data := []byte(`{"luna":{"url":"ws://host:8879/terminal","token":"private-credential-longer-than-32-characters"},"screen":{"clock_24":"wrong type"}}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := Load(path)
	if err == nil {
		t.Fatal("expected type error")
	}
	if !reflect.DeepEqual(st.Get().Luna, Luna{}) {
		t.Fatal("partially parsed Luna credentials remained active")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("partial config not secured: %v %v", info, err)
	}
}

func TestLunaSecretsAndOwnerOnlyFile(t *testing.T) {
	if Defaults().Luna.Enabled() {
		t.Fatal("Luna enabled by default")
	}
	cfg := Defaults()
	cfg.Luna = Luna{URL: "ws://luna.local:8879/terminal", Token: "owner-only-luna-terminal-credential", WakeAssistants: map[string]string{"hey_odin": "Luna", "galileo": "Astra"}}
	path := filepath.Join(t.TempDir(), "echod.json")
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(st.Get().Luna, cfg.Luna) {
		t.Fatal("Luna settings not loaded")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("credential mode: %v %v", info, err)
	}
	exported, err := Export(cfg, "echo5", "test")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(exported), cfg.Luna.Token) {
		t.Fatal("export leaked Luna token")
	}
	cleaned := cfg
	ClearSecrets(&cleaned)
	if cleaned.Luna.Token != "" {
		t.Fatal("ClearSecrets retained token")
	}
	KeepSecrets(&cleaned, cfg)
	if cleaned.Luna.Token != cfg.Luna.Token {
		t.Fatal("same endpoint lost token")
	}
	cleaned.Luna.URL = "ws://elsewhere:8879/terminal"
	cleaned.Luna.Token = ""
	KeepSecrets(&cleaned, cfg)
	if cleaned.Luna.Token != "" {
		t.Fatal("credential transferred to another endpoint")
	}
	imported := Defaults()
	imported.Luna = Luna{URL: "ws://attacker/terminal", Token: "unexpected"}
	KeepOwn(&imported, cfg)
	if !reflect.DeepEqual(imported.Luna, cfg.Luna) {
		t.Fatal("settings import changed room endpoint")
	}
}

func TestLunaInvalidWakeMappingFailsClosed(t *testing.T) {
	for _, mapping := range []string{
		`{"galileo":"Galileo"}`, `{"galileo":"astra"}`, `{"galileo":""}`,
		`{"":"Luna"}`, `{"galileo":null}`, `{"galileo":3}`, `[]`, `true`,
	} {
		t.Run(mapping, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			raw := `{"luna":{"url":"ws://host:8879/terminal","token":"synthetic-credential-longer-than-32-characters","wake_assistants":` + mapping + `}}`
			if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
				t.Fatal(err)
			}
			st, err := Load(path)
			if err == nil || !reflect.DeepEqual(st.Get().Luna, Luna{}) {
				t.Fatal("invalid mapping retained a partial backend")
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatal("invalid mapping left credentials unprotected")
			}
		})
	}
}
