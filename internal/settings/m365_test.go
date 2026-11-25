package settings

import "testing"

func TestM365IsKeptLocal(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := Load()
	s.M365 = true
	s.M365KeyID = "local-key"
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if !got.M365 || got.M365KeyID != "local-key" {
		t.Fatalf("saved M365 state: %+v", got)
	}

	from := Settings{Theme: "dark"}
	from.KeepOwn(Settings{M365: true, M365KeyID: "other-key"})
	if !from.M365 || from.M365KeyID != "other-key" || from.Theme != "dark" {
		t.Fatalf("KeepOwn kept M365 from another computer: %+v", from)
	}
	from = Settings{M365: true, M365KeyID: "other-key"}
	from.KeepOwn(Settings{})
	if from.M365 || from.M365KeyID != "" {
		t.Fatalf("KeepOwn did not clear another computer's M365: %+v", from)
	}
}
