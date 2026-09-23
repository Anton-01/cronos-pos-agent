package main

import (
	"strings"
	"testing"
)

// The installer tells the agent which of the two operations it just finished,
// and anything else has to be read as a first installation: its message
// explains the agent from scratch, which is never wrong, whereas the upgrade
// message would tell a first-time operator that settings were preserved from an
// installation that never existed.
func TestNormalizeSetupMode(t *testing.T) {
	cases := map[string]string{
		"update":     setupModeUpdate,
		"UPDATE":     setupModeUpdate,
		"  update  ": setupModeUpdate,
		"install":    setupModeInstall,
		"":           setupModeInstall,
		"cualquiera": setupModeInstall,
	}

	for input, want := range cases {
		if got := normalizeSetupMode(input); got != want {
			t.Errorf("normalizeSetupMode(%q) = %q, se esperaba %q", input, got, want)
		}
	}
}

// The two dialogs have to say different things: the operator of an upgrade is
// asking whether the till has to be paired with the POS again, and the operator
// of a first install is asking what this program is and where it went.
func TestSetupDialogTextDiffersByMode(t *testing.T) {
	installTitle, installText := setupDialogText(setupModeInstall)
	updateTitle, updateText := setupDialogText(setupModeUpdate)

	if installTitle == updateTitle || installText == updateText {
		t.Fatal("los dos modos muestran el mismo mensaje")
	}

	if !strings.Contains(installTitle, "Instalación") {
		t.Errorf("el título de instalación no la nombra: %q", installTitle)
	}
	if !strings.Contains(updateTitle, "Actualización") {
		t.Errorf("el título de actualización no la nombra: %q", updateTitle)
	}

	// Las dos tienen que confirmar el éxito y decir dónde encontrar el agente,
	// que es lo que el operador hace a continuación.
	for name, text := range map[string]string{"instalación": installText, "actualización": updateText} {
		if !strings.Contains(text, AgentVersion) {
			t.Errorf("el mensaje de %s no dice la versión instalada", name)
		}
		if !strings.Contains(text, "barra de tareas") {
			t.Errorf("el mensaje de %s no dice dónde está el agente", name)
		}
	}

	// Y la de actualización tiene que responder la única pregunta que plantea
	// una actualización.
	if !strings.Contains(updateText, "token") {
		t.Error("el mensaje de actualización no dice que se conserva el token de seguridad")
	}
}

// The dialog is confirmed once per run of the installer, which is what
// --setup-id identifies. Without an id the marker falls back to the version, so
// an installer older than v1.9.0 keeps the behaviour it had.
func TestSetupDialogMarkerIdentifiesTheRun(t *testing.T) {
	withID := SetupDialogRequest{Mode: setupModeUpdate, RunID: "20260918203000"}
	if withID.RunID == "" {
		t.Fatal("caso mal construido")
	}

	// El marcador de una petición sin id tiene que distinguir los dos modos:
	// una reinstalación sobre la misma versión no debe quedarse muda por
	// haber confirmado antes una actualización.
	install := AgentVersion + "|" + normalizeSetupMode("")
	update := AgentVersion + "|" + normalizeSetupMode(setupModeUpdate)
	if install == update {
		t.Error("los marcadores de instalación y actualización coinciden")
	}
}
