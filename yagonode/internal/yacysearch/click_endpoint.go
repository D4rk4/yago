package yacysearch

import (
	"context"
	"net/http"
	"strconv"
	"strings"
)

const (
	pathSearchClick           = "/searchclick"
	maximumClickBodyBytes     = 70000
	maximumClickTokenBytes    = 65536
	maximumClickIdentityBytes = 2048
)

type ImpressionCandidate struct {
	URLIdentity     string
	ClusterIdentity string
	Position        int
	LexicalPosition int
}

type PreparedImpression struct {
	Token string
	Order []int
}

type ImpressionRecorder interface {
	PrepareImpression(
		ctx context.Context,
		query string,
		candidates []ImpressionCandidate,
	) (PreparedImpression, error)
	RecordClick(ctx context.Context, token, urlIdentity string, position int) error
}

type ClickCapture struct {
	Enabled  bool
	Recorder ImpressionRecorder
}

type clickEndpoint struct {
	recorder ImpressionRecorder
}

func (e clickEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maximumClickBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)

		return
	}
	token := r.PostForm.Get("t")
	identity := r.PostForm.Get("i")
	position, err := strconv.Atoi(r.PostForm.Get("p"))
	if err != nil || position < 1 || token == "" || identity == "" ||
		len(token) > maximumClickTokenBytes || len(identity) > maximumClickIdentityBytes ||
		strings.TrimSpace(identity) != identity {
		http.Error(w, "invalid impression click", http.StatusBadRequest)

		return
	}
	_ = e.recorder.RecordClick(r.Context(), token, identity, position)
	w.WriteHeader(http.StatusNoContent)
}
