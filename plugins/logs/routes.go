package logs

import (
	"net/http"

	"github.com/appbaseio/arc/plugins"
)

func (l *Logs) routes() []plugins.Route {
	middleware := (&chain{}).Wrap
	return []plugins.Route{
		{
			Name:        "Get index logs",
			Methods:     []string{http.MethodGet},
			Path:        "/{index}/_logs",
			HandlerFunc: middleware(l.getLogs()),
			Description: "Returns the logs for an index",
		},
		{
			Name:        "Get logs",
			Methods:     []string{http.MethodGet},
			Path:        "/_logs",
			HandlerFunc: middleware(l.getLogs()),
			Description: "Returns the logs for the cluster",
		},
		{
			Name:        "Get index logs for search requests",
			Methods:     []string{http.MethodGet},
			Path:        "/{index}/_logs/search",
			HandlerFunc: middleware(l.getSearchLogs()),
			Description: "Returns the search request logs for an index",
		},
		{
			Name:        "Get logs for search requests",
			Methods:     []string{http.MethodGet},
			Path:        "/_logs/search",
			HandlerFunc: middleware(l.getSearchLogs()),
			Description: "Returns the search request logs for the cluster",
		},
	}
}
