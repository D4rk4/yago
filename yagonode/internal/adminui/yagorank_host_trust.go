package adminui

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"
)

type HostTrustView struct {
	Blend   float64
	Domains []string
}

type HostTrustSource interface {
	HostTrust(context.Context) HostTrustView
	ApplyHostTrust(context.Context, HostTrustView) error
}

type hostTrustStatusView struct {
	Blend       string
	Domains     string
	DomainCount int
}

func hostTrustStatus(
	ctx context.Context,
	ranking RankingSource,
	override *hostTrustStatusView,
) *hostTrustStatusView {
	source, ok := ranking.(HostTrustSource)
	if !ok {
		return nil
	}
	if override != nil {
		return override
	}
	trust := source.HostTrust(ctx)

	return &hostTrustStatusView{
		Blend:       strconv.FormatFloat(trust.Blend, 'g', -1, 64),
		Domains:     strings.Join(trust.Domains, "\n"),
		DomainCount: len(trust.Domains),
	}
}

func (c *Console) applyYagoRankHostTrust(w http.ResponseWriter, r *http.Request) {
	source, ok := c.ranking.(HostTrustSource)
	if !ok {
		c.renderYagoRank(w, r, yagorankView{
			weights: c.ranking.Profile(r.Context()).Weights,
			errMsg:  "Host trust settings are not available.",
		})

		return
	}
	blendText := strings.TrimSpace(r.PostFormValue("trust_blend"))
	domainText := strings.TrimSpace(r.PostFormValue("trust_domains"))
	domains := strings.Fields(domainText)
	form := &hostTrustStatusView{
		Blend: blendText, Domains: domainText, DomainCount: len(domains),
	}
	blend, err := strconv.ParseFloat(blendText, 64)
	if err != nil {
		c.renderYagoRank(w, r, yagorankView{
			weights: c.ranking.Profile(r.Context()).Weights,
			trust:   form,
			errMsg:  "Enter a number for trust blend.",
		})

		return
	}
	if math.IsNaN(blend) || math.IsInf(blend, 0) || blend < 0 || blend > 1 {
		c.renderYagoRank(w, r, yagorankView{
			weights: c.ranking.Profile(r.Context()).Weights,
			trust:   form,
			errMsg:  "Trust blend must be between zero and one.",
		})

		return
	}
	if err := source.ApplyHostTrust(r.Context(), HostTrustView{
		Blend: blend, Domains: domains,
	}); err != nil {
		c.renderYagoRank(w, r, yagorankView{
			weights: c.ranking.Profile(r.Context()).Weights,
			trust:   form,
			errMsg:  "Save host trust failed: " + err.Error(),
		})

		return
	}
	c.renderYagoRank(w, r, yagorankView{
		weights: c.ranking.Profile(r.Context()).Weights,
		notice:  "Host trust policy saved.",
	})
}
