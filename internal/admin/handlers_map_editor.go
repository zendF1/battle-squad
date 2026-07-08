package admin

import (
	"net/http"
	"strings"

	"battle-squad/internal/shared/observability"
)

// ---------------------------------------------------------------------------
// Brick Types Handlers
// ---------------------------------------------------------------------------

func (s *Server) handleBrickTypesList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	items, err := s.repo.GetBrickTypes(ctx)
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to list brick types")
	}

	s.render(w, "brick_types", map[string]interface{}{
		"ActivePage": "brick-types",
		"Items":      items,
		"Flash":      r.URL.Query().Get("flash"),
		"Error":      r.URL.Query().Get("error"),
	})
}

func (s *Server) handleBrickTypeEdit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.URL.Query().Get("id")
	isNew := id == ""

	var bt ConfigBrickType
	if !isNew {
		found, err := s.repo.GetBrickType(ctx, id)
		if err != nil {
			http.Redirect(w, r, "/brick-types?error=Brick+type+not+found", http.StatusSeeOther)
			return
		}
		bt = *found
	}

	s.render(w, "brick_type_edit", map[string]interface{}{
		"ActivePage": "brick-types",
		"BrickType":  bt,
		"IsNew":      isNew,
		"Error":      r.URL.Query().Get("error"),
	})
}

func (s *Server) handleBrickTypeSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/brick-types?error=Invalid+form+data", http.StatusSeeOther)
		return
	}
	ctx := r.Context()

	bt := &ConfigBrickType{
		BrickTypeID:  strings.TrimSpace(r.FormValue("brick_type_id")),
		Name:         r.FormValue("name"),
		Destructible: r.FormValue("destructible") == "true",
	}
	if bt.BrickTypeID == "" {
		http.Redirect(w, r, "/brick-types/edit?error=Brick+Type+ID+is+required", http.StatusSeeOther)
		return
	}
	if err := s.repo.UpsertBrickType(ctx, bt); err != nil {
		observability.Log.Error().Err(err).Msg("failed to upsert brick type")
		http.Redirect(w, r, "/brick-types?error=Failed+to+save+brick+type", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/brick-types?flash=Saved+successfully", http.StatusSeeOther)
}

func (s *Server) handleBrickTypeDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/brick-types?error=Invalid+form+data", http.StatusSeeOther)
		return
	}
	ctx := r.Context()
	id := r.FormValue("id")
	if id == "" {
		http.Redirect(w, r, "/brick-types?error=Missing+ID", http.StatusSeeOther)
		return
	}

	if err := s.repo.DeleteBrickType(ctx, id); err != nil {
		observability.Log.Error().Err(err).Msg("failed to delete brick type")
		http.Redirect(w, r, "/brick-types?error=Failed+to+delete", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/brick-types?flash=Deleted+successfully", http.StatusSeeOther)
}
