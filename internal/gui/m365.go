package gui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"slices"
	"time"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/settings"
)

const m365KeyName = "Claude for Microsoft 365"

var (
	removeM365Certificate = gateway.RemoveM365Certificate
	restartM365Gateway    = restartGateway
	stopM365Gateway       = func(ctx context.Context) error { return m365.Stop(ctx) }
	m365AutomaticTrust    = runtime.GOOS == "darwin"
)

// ensureM365Key keeps one named, revocable key for the Office add-in. Its
// secret is returned only by the explicit copy route.
func ensureM365Key(s *settings.Settings) error {
	keys, err := access.List()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(keys, func(k access.Key) bool { return k.ID == s.M365KeyID })
	if i >= 0 {
		if keys[i].Off {
			if _, err := access.Update("on-key", access.Change{Key: keys[i].ID}); err != nil {
				return err
			}
		}
		return nil
	}
	if _, err := access.Update("add-key", access.Change{Name: m365KeyName}); err != nil {
		return err
	}
	keys, err = access.List()
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return errors.New("the Microsoft 365 gateway key was not saved")
	}
	s.M365KeyID = keys[len(keys)-1].ID
	return nil
}

type m365OffWarnings struct {
	stop, trust error
}

func (w m365OffWarnings) err() error { return errors.Join(w.stop, w.trust) }

// setM365Off commits the security boundary first: the origin is dropped and
// the listener is stopped even when trust-store cleanup is refused.
func setM365Off(ctx context.Context, s *settings.Settings) (m365OffWarnings, error) {
	s.M365 = false
	s.CORSOrigins = slices.DeleteFunc(s.CORSOrigins, func(origin string) bool { return origin == gateway.M365Origin })
	if err := settings.Save(*s); err != nil {
		return m365OffWarnings{}, err
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	warnings := m365OffWarnings{stop: stopM365Gateway(stopCtx)}
	cancel()
	if m365AutomaticTrust {
		_, warnings.trust = removeM365Certificate(ctx)
	} else {
		warnings.trust = errors.New("automatic certificate trust removal is available only on macOS; if you installed Magpie M365 Local CA, remove it from the current user's trust store by hand")
	}
	return warnings, nil
}

func m365Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/settings/m365", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ On bool }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(w, err)
			return
		}
		if in.On && served.Load() == nil {
			fail(w, errors.New("this magpie is not serving the gateway; take it over or restart it here first"))
			return
		}
		s := settings.Load()
		if in.On {
			m365.ClearError()
			if err := ensureM365Key(&s); err != nil {
				fail(w, err)
				return
			}
			if !slices.Contains(s.CORSOrigins, gateway.M365Origin) {
				s.CORSOrigins = append(s.CORSOrigins, gateway.M365Origin)
			}
			s.M365 = true
			if err := settings.Save(s); err != nil {
				fail(w, err)
				return
			}
			stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = stopM365Gateway(stopCtx)
			cancel()
			if out := restartM365Gateway(); !out.OK {
				restartErr := errors.New("the gateway could not restart: " + out.Reason + " " + out.Error)
				warnings, cleanupErr := setM365Off(r.Context(), &s)
				if cleanupErr == nil {
					cleanupErr = warnings.err()
				}
				if cleanupErr != nil {
					restartErr = fmt.Errorf("%w; cleanup failed: %v", restartErr, cleanupErr)
				}
				fail(w, restartErr)
				return
			}
			if !m365.Status().Running {
				startErr := m365.Status().Error
				if startErr == "" {
					startErr = "the listener did not start"
				}
				warnings, cleanupErr := setM365Off(r.Context(), &s)
				if cleanupErr == nil {
					cleanupErr = warnings.err()
				}
				if cleanupErr != nil {
					startErr += "; cleanup failed: " + cleanupErr.Error()
				}
				fail(w, errors.New("Claude for Microsoft 365 could not start: "+startErr))
				return
			}
			writeJSON(w, map[string]any{"settings": settingsState()})
			return
		}

		warnings, err := setM365Off(r.Context(), &s)
		if err != nil {
			fail(w, err)
			return
		}
		out := map[string]any{"settings": settingsState()}
		if warnings.stop != nil {
			out["stopWarning"] = warnings.stop.Error()
		}
		if warnings.trust != nil {
			out["trustWarning"] = warnings.trust.Error()
		}
		writeJSON(w, out)
	})

	mux.HandleFunc("POST /api/settings/m365/key", func(w http.ResponseWriter, r *http.Request) {
		s := settings.Load()
		if s.M365KeyID == "" {
			fail(w, errors.New("turn on Claude for Microsoft 365 first"))
			return
		}
		secret, err := access.Update("copy-key", access.Change{Key: s.M365KeyID})
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, map[string]string{"secret": secret})
	})

	mux.HandleFunc("POST /api/settings/m365/certificate/{action}", func(w http.ResponseWriter, r *http.Request) {
		if !m365AutomaticTrust {
			fail(w, errors.New("automatic certificate trust management is available only on macOS; install or remove the shown certificate in the current user's trust store by hand"))
			return
		}
		var (
			status gateway.M365CertificateStatus
			err    error
		)
		switch r.PathValue("action") {
		case "install":
			status, err = gateway.InstallM365Certificate(r.Context())
		case "remove":
			status, err = gateway.RemoveM365Certificate(r.Context())
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, status)
	})
}

// m365State is what Settings may show without revealing the gateway key.
func m365State() (gateway.M365Status, string, string, bool) {
	s := settings.Load()
	status := m365.Status()
	var name, masked string
	if keys, err := access.List(); err == nil {
		if i := slices.IndexFunc(keys, func(k access.Key) bool { return k.ID == s.M365KeyID }); i >= 0 {
			name, masked = keys[i].Name, keys[i].Masked
		}
	}
	return status, name, masked, m365AutomaticTrust
}
