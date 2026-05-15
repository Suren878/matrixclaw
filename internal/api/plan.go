package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSessionPlan(w http.ResponseWriter, r *http.Request, sessionID string) {
	switch r.Method {
	case http.MethodGet:
		plan, err := s.core.SessionPlan(r.Context(), sessionID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.SessionPlanResponse{Plan: plan})
	case http.MethodPatch:
		var req core.UpdateSessionPlanRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		var (
			plan core.SessionPlan
			err  error
		)
		if req.Clear {
			plan, err = s.core.ClearSessionPlan(r.Context(), sessionID)
		} else if req.Goal != nil {
			plan, err = s.core.SetSessionGoal(r.Context(), sessionID, *req.Goal)
		} else {
			plan, err = s.core.SessionPlan(r.Context(), sessionID)
		}
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.SessionPlanResponse{Plan: plan})
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPatch)
	}
}

func (s *Server) handleSessionPlanItems(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	var req core.AddPlanItemRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}
	plan, err := s.core.AddPlanItem(r.Context(), sessionID, req.Text, req.ParentID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, core.SessionPlanResponse{Plan: plan})
}

func (s *Server) handleSessionPlanStatus(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPatch {
		writeMethodNotAllowed(w, http.MethodPatch)
		return
	}
	var req core.UpdatePlanItemRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}
	plan, err := s.core.UpdatePlanItem(r.Context(), sessionID, req.ItemID, core.PlanItemStatus(req.Status), req.Text)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.SessionPlanResponse{Plan: plan})
}

func (s *Server) handleSessionPlanRun(w http.ResponseWriter, r *http.Request, sessionID string) {
	switch r.Method {
	case http.MethodGet:
		planRun, err := s.core.SessionPlanRun(r.Context(), sessionID)
		if err != nil {
			writeError(w, err)
			return
		}
		plan, err := s.core.SessionPlan(r.Context(), sessionID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.PlanRunResponse{PlanRun: planRun, Plan: plan})
	case http.MethodPost:
		var req core.PlanRunStartRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		planRun, plan, err := s.core.StartSessionPlanRun(r.Context(), sessionID, req.Reset)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.PlanRunResponse{PlanRun: planRun, Plan: plan})
	case http.MethodPatch:
		var req core.PlanRunBindRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		if err := s.core.BindSessionPlanRunStep(r.Context(), sessionID, req.RunID); err != nil {
			writeError(w, err)
			return
		}
		planRun, err := s.core.SessionPlanRun(r.Context(), sessionID)
		if err != nil {
			writeError(w, err)
			return
		}
		plan, err := s.core.SessionPlan(r.Context(), sessionID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.PlanRunResponse{PlanRun: planRun, Plan: plan})
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost, http.MethodPatch)
	}
}
