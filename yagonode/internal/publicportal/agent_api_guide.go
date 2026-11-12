package publicportal

import (
	"embed"
	"html/template"
	"net/http"
)

//go:embed templates/agent_api.tmpl
var agentAPIGuideFS embed.FS

var agentAPIGuideTemplate = template.Must(
	template.ParseFS(agentAPIGuideFS, "templates/agent_api.tmpl"),
)

func AgentAPIGuide() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api" {
			http.NotFound(w, r)

			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)

			return
		}

		w.Header().Set("Content-Type", htmlType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)

			return
		}
		if err := agentAPIGuideTemplate.ExecuteTemplate(w, "agent-api", nil); err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	})
}
