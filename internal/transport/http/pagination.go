package httptransport

import (
	"net/http"

	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/transport/http/jsonapi"
)

func writeResourceCollection(w http.ResponseWriter, r *http.Request, resources []jsonapi.Resource, defaultLimit int, maxLimit int) {
	page := jsonapi.ParsePage(r, defaultLimit, maxLimit)
	paged, nextCursor := jsonapi.PageResources(resources, page)
	jsonapi.Write(w, http.StatusOK, jsonapi.Document{
		Data:  paged,
		Links: jsonapi.PageLinks(r, nextCursor),
		Meta:  jsonapi.PageMeta(len(paged), nextCursor),
	})
}
