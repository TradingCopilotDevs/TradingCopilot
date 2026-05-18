package httptransport

import (
	"net/http"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/transport/http/jsonapi"
)

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	payload, err := s.dashboardUsecase.Load(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "dashboard-load-failed", "Dashboard load failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("dashboards", "current", payload))
}
