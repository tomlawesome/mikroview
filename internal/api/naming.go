// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"strconv"

	"github.com/tomlawesome/mikroview/internal/entities"
	"github.com/tomlawesome/mikroview/internal/naming"
)

// nameProvenanceResponse answers "where does the name shown for this
// token come from, and would labelling it here change anything?".
//
// Editable is the field this endpoint exists for (issue #413). The
// owner's 2026-08-22 ruling kept "RouterOS always wins" (#186 step 4c):
// a mikroview-side label for a host RouterOS already names is stored
// faithfully and then never displayed. POST /api/entities accepts that
// write and reports success, because from its point of view the write
// did succeed -- which is exactly how an operator ends up typing a name,
// seeing a confirmation, and watching nothing change. The editor
// therefore has to ask *before* offering a field, and this is what it
// asks.
//
// So Editable=false is not advice: it is the instruction to render an
// explanation instead of an input. Source and Router say what to put in
// that explanation -- which pushed table holds the winning name, and on
// which device -- since "change it on the router" is not actionable
// without both.
type nameProvenanceResponse struct {
	Type   string `json:"type"`
	Key    string `json:"key"`
	Device string `json:"device,omitempty"`
	// Name is what is displayed for this key right now, "" if nothing
	// names it.
	Name string `json:"name"`
	// Source is one of internal/naming's Source* values.
	Source string `json:"source"`
	// Label is the operator's own entity label for this key, reported
	// even when it lost to a router-pushed name -- the editor says
	// "your label 'nas' is not what is shown" rather than silently
	// presenting an empty field over the top of a saved label.
	Label string `json:"label"`
	// Editable is false when saving a label here would have no visible
	// effect. See this type's doc comment.
	Editable bool `json:"editable"`
	// Router is the device whose pushed table supplies the winning
	// name, set only when Editable is false -- the place to go and
	// change it.
	Router string `json:"router,omitempty"`
}

// handleNameProvenance answers for one (type, key) token.
//
// User-tier (#653), matching GET/POST/DELETE /api/entities and the
// editor it serves: #413 gives a viewer role no pencil at all rather
// than a disabled one, so a viewer has no reason to reach this, and the
// response is a partial map of which router names which host -- the
// same administrative metadata the entities list is gated for. Widened
// from admin to user by #653's "watchers" bench ruling, same as the
// entities surface it serves.
//
// Reads only what mikroview already holds (the entity store, the config
// maps, and state the router pushed); nothing here contacts a device,
// per AGENTS.md's observe-never-probe invariant.
func (s *Server) handleNameProvenance(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}

	q := r.URL.Query()
	entityType := q.Get("type")
	key := q.Get("key")
	device := q.Get("device")

	if key == "" {
		http.Error(w, "key is required", http.StatusBadRequest)
		return
	}

	var p naming.Provenance
	switch entityType {
	case entities.TypeHost:
		// device scopes the router-pushed layer and nothing else (see
		// naming.Resolver.Host). An empty device therefore reports the
		// mikroview-side answer, which is the honest one: with no
		// device known, no router's claim may be applied.
		p = s.Naming.HostProvenance(device, key)
	case entities.TypeRule:
		p = s.Naming.RuleProvenance(key)
	case entities.TypePort:
		// A key that is not a port number resolves to nothing rather
		// than erroring: naming.Resolver.Port already treats port <= 0
		// as a miss, and this endpoint is a question, not a mutation.
		port, err := strconv.Atoi(key)
		if err != nil {
			port = 0
		}
		p = s.Naming.PortProvenance(port)
	default:
		http.Error(w, "unknown entity type", http.StatusBadRequest)
		return
	}

	resp := nameProvenanceResponse{
		Type:     entityType,
		Key:      key,
		Device:   device,
		Name:     p.Name,
		Source:   p.Source,
		Label:    p.Label,
		Editable: !p.RouterWins(),
	}
	if !resp.Editable {
		resp.Router = device
	}
	writeJSON(w, http.StatusOK, resp)
}
