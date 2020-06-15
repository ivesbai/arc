package elasticsearch

import (
	"bytes"
	"io"
	"io/ioutil"
	"net/http"

	log "github.com/sirupsen/logrus"

	"github.com/appbaseio/arc/util"
	es7 "github.com/olivere/elastic/v7"
)

func (es *elasticsearch) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Forward the request to elasticsearch
		esClient := util.GetClient7()

		// remove content-type header from r.Headers as that is internally managed my oliver
		// and can give following error if passed `{"error":{"code":500,"message":"elastic: Error 400 (Bad Request): java.lang.IllegalArgumentException: only one Content-Type header should be provided [type=content_type_header_exception]","status":"Internal Server Error"}}`
		headers := http.Header{}
		for k, v := range r.Header {
			if k != "Content-Type" {
				headers.Set(k, v[0])
			}
		}

		esBody, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Errorln(logTag, ":", err)
		}

		requestOptions := es7.PerformRequestOptions{
			Method:  r.Method,
			Path:    r.URL.Path,
			Params:  r.URL.Query(),
			Headers: headers,
			Body:    string(esBody),
		}

		response, err := esClient.PerformRequest(ctx, requestOptions)

		if err != nil {
			log.Errorln(logTag, ": error fetching response for", r.URL.Path, err)
			util.WriteBackError(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Copy the headers
		for k, v := range response.Header {
			if k != "Content-Length" {
				w.Header().Set(k, v[0])
			}
		}
		w.Header().Set("X-Origin", "ES")

		// Copy the status code
		w.WriteHeader(response.StatusCode)

		// Copy the body
		io.Copy(w, bytes.NewReader(response.Body))
	}
}
