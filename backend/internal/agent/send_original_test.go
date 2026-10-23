package agent

import (
	"context"
	"errors"
	"testing"

	"agentdeck/internal/store"
)

type validatedSendFixture struct {
	desktopFixture
	validated int
	failure   error
}

func (d *validatedSendFixture) SendValidatedInput(_ context.Context, id, text, request, project string, _ TurnOptions, _ []any) error {
	d.validated++
	if project != d.thread.Project || id != d.thread.ID {
		return errors.New("wrong project/thread")
	}
	if d.failure != nil {
		return d.failure
	}
	d.sentID, d.text = id, text
	return nil
}

func TestOriginalCodexSendValidatesAndAdoptsWithoutSeparateCreate(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "blocked"}[reject], func(t *testing.T) {
			m := connectorManager(t)
			project := m.Projects()[0].Path
			ss := store.Session{ID: "original", Agent: "codex", Project: project, Source: "log", Status: "completed"}
			if e := m.Store.Put("sessions", ss.ID, ss); e != nil {
				t.Fatal(e)
			}
			d := &validatedSendFixture{desktopFixture: desktopFixture{thread: DesktopThread{ID: ss.ID, Project: project, Kind: "codex", Host: "local"}, reject: true}}
			if reject {
				d.failure = &InputNotSentError{Err: errors.New("native restriction")}
			}
			m.Desktop = d
			t.Setenv("PATH", t.TempDir())
			e := m.Send(ss.ID, "Synthetic input", "synthetic-request")
			if d.reads != 0 || d.validated != 1 || len(m.runs) != 0 {
				t.Fatal("redundant discovery or competing executor")
			}
			var got store.Session
			if err := m.Store.Get("sessions", ss.ID, &got); err != nil {
				t.Fatal(err)
			}
			if reject {
				if e == nil || got.Managed || got.Source != "log" || d.sentID != "" {
					t.Fatal("rejected input adopted original or bypassed restriction")
				}
			} else if e != nil || !got.Managed || got.Source != "desktop" || d.sentID != ss.ID {
				t.Fatal("validated send failed to retain/adopt original", e)
			}
		})
	}
}
