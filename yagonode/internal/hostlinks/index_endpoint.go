package hostlinks

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

const indexContentType = "application/json; charset=UTF-8"

type endpoint struct {
	networkName string
	status      RuntimeStatus
	links       IncomingHostLinks
}

type indexResponse struct {
	Version       string                       `json:"version"`
	Uptime        string                       `json:"uptime"`
	Name          string                       `json:"name"`
	RowDefinition string                       `json:"rowdef"`
	Index         map[string][]json.RawMessage `json:"idx"`
}

func (e endpoint) Serve(
	ctx context.Context,
	req yagoproto.IndexRequest,
) (httpguard.RawResponse, error) {
	resp := indexResponse{
		Version: e.status.Version(ctx),
		Uptime:  strconv.Itoa(e.status.Uptime(ctx)),
		Index:   map[string][]json.RawMessage{},
	}
	if e.accepts(req) {
		graph := e.links.IncomingHostLinks(ctx)
		resp.Name = yagoproto.IndexObjectHost
		resp.RowDefinition = graph.RowDefinition
		resp.Index = graphIndex(graph)
	}

	var body strings.Builder
	if err := json.NewEncoder(&body).Encode(resp); err != nil {
		return httpguard.RawResponse{}, fmt.Errorf("encode host link index: %w", err)
	}

	return httpguard.RawResponse{
		ContentType: indexContentType,
		Body:        body.String(),
	}, nil
}

func (e endpoint) accepts(req yagoproto.IndexRequest) bool {
	if yagoproto.NetworkUnit(req.NetworkName) != yagoproto.NetworkUnit(e.networkName) {
		return false
	}

	return req.Object == yagoproto.IndexObjectHost
}

func graphIndex(graph Graph) map[string][]json.RawMessage {
	index := make(map[string][]json.RawMessage, len(graph.LinkedHosts))
	for _, host := range graph.LinkedHosts {
		if host.HostHash == "" {
			continue
		}
		index[host.HostHash] = append([]json.RawMessage(nil), host.References...)
	}

	return index
}
