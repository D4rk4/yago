package adminauth

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

type fixedLoginNodeStatusSource struct {
	status LoginNodeStatus
}

func (s fixedLoginNodeStatusSource) LoginNodeStatus(context.Context) LoginNodeStatus {
	return s.status
}

func loginNodeStatusService(
	t *testing.T,
	status LoginNodeStatusSource,
) (*Service, *scriptedEngine) {
	t.Helper()
	engine := newScriptedEngine()
	service, err := New(scriptedVault(t, engine), Config{LoginNodeStatus: status})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return service, engine
}

func TestLoginPageUsesNarrowNodeStatusAndBlankPrincipal(t *testing.T) {
	t.Parallel()

	service, engine := loginNodeStatusService(t, fixedLoginNodeStatusSource{status: LoginNodeStatus{
		NodeName:       "search-node",
		SwarmAddress:   "search.example:8090",
		ProcessorModel: "Intel Xeon Gold 6338N",
		ProcessorCount: "8 logical CPUs",
		MemoryCapacity: "31.2 GiB total · 12.0 GiB free",
		DataFreeSpace:  "975.0 GiB",
		Version:        "v0.0.8",
		Uptime:         "2h3m4s",
	}})
	injectAdmin(t, engine, "private-operator", "correct-horse")
	page := doRequest(htmlSurface(t, service), http.MethodGet, PathLoginPage, "")
	body := page.Body.String()
	for _, want := range []string{
		`<strong>YaGo Search</strong>`,
		`<span>OpenSource Search Engine</span>`,
		`<label for="u">Login:</label>`,
		`<label for="p">Password:</label>`,
		`<button type="submit" aria-label="Sign in">GO</button>`,
		`<h2>search-node</h2>`,
		`<dt>Swarm address</dt><dd>search.example:8090</dd>`,
		`<dt>Processors</dt><dd><span>Intel Xeon Gold 6338N</span><span>8 logical CPUs</span></dd>`,
		`<dt>Memory</dt><dd>31.2 GiB total · 12.0 GiB free</dd>`,
		`<dt>Data storage free</dt><dd>975.0 GiB</dd>`,
		`YaGo v0.0.8`,
		`Node uptime: 2h3m4s`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("login page missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"private-operator",
		"<datalist",
		"Shutdown",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("login page exposed %q", forbidden)
		}
	}
	if !strings.Contains(body, `name="username" autocomplete="username"`) ||
		!strings.Contains(body, `type="password" autocomplete="current-password"`) ||
		strings.Contains(body, `name="username" value=`) || strings.Count(body, "<input") != 2 {
		t.Fatalf("login intake controls are not the bounded blank form: %s", body)
	}
}

func TestLoginFormKeepsExplicitRowsAndThreeRegionsAtNarrowWidth(t *testing.T) {
	t.Parallel()

	service, engine := loginNodeStatusService(t, nil)
	injectAdmin(t, engine, "operator", "correct-horse")
	body := doRequest(htmlSurface(t, service), http.MethodGet, PathLoginPage, "").Body.String()
	loginRow := `<div class="login-form__row">`
	secondRow := `<div class="login-form__row login-form__row--password">`
	if strings.Count(body, loginRow) != 1 || strings.Count(body, secondRow) != 1 ||
		strings.Index(body, loginRow) > strings.Index(body, secondRow) {
		t.Fatalf("login rows are not explicit and ordered: %s", body)
	}

	stylesheet, err := authTemplateFS.ReadFile("assets/auth.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	for _, want := range []string{
		`.login-form__row {`,
		`.login-product {`,
		`grid-column: 2 / 4;`,
		`padding: 0 0.75rem 1rem;`,
		`font-size: 1.125rem; font-weight: 500; line-height: 1.05;`,
		`grid-template-columns: 5.25rem minmax(10rem, 11.5rem) 1.9rem;`,
		`.login-form__row button {`,
		`width: 1.9rem;`,
		`height: 1.9rem;`,
		`border-radius: 3px;`,
		`background: linear-gradient(to bottom, #596b79, #3d4b55);`,
		`.login-form button:active { background: #465663; box-shadow: var(--auth-sink); transform: translate(1px, 1px); }`,
		`.login-status dd span { display: block; }`,
		`background: linear-gradient(to bottom, var(--auth-masthead) 0 22.5%, var(--auth-blue) 22.5% 77.5%, var(--auth-masthead) 77.5% 100%);`,
		`@media (max-width: 55rem) {`,
		`@media (max-width: 24rem) {`,
		`justify-self: start;`,
		`.login-form__row { grid-template-columns: 4.5rem minmax(0, 1fr) 1.9rem; gap: 0.3rem; }`,
		`background: var(--auth-masthead);`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("auth.css missing responsive login contract %q", want)
		}
	}
}

func TestLoginNodeStatusUnavailableAndEscaped(t *testing.T) {
	t.Parallel()

	missing := normalizedLoginNodeStatus(context.Background(), nil)
	for name, value := range map[string]string{
		"node": missing.NodeName, "swarm": missing.SwarmAddress,
		"processor model": missing.ProcessorModel, "processors": missing.ProcessorCount,
		"storage": missing.DataFreeSpace, "version": missing.Version,
		"uptime": missing.Uptime,
	} {
		if value != loginNodeStatusUnavailable {
			t.Errorf("%s = %q", name, value)
		}
	}

	malicious := `<script>alert("status")</script>`
	service, engine := loginNodeStatusService(t, fixedLoginNodeStatusSource{status: LoginNodeStatus{
		NodeName: malicious,
	}})
	injectAdmin(t, engine, "operator", "correct-horse")
	body := doRequest(htmlSurface(t, service), http.MethodGet, PathLoginPage, "").Body.String()
	if strings.Contains(body, malicious) || !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("node status was not escaped: %s", body)
	}
	if !strings.Contains(body, `<dt>Swarm address</dt><dd>Unavailable</dd>`) {
		t.Fatal("missing status fields did not degrade independently")
	}
}

func TestLoginNodeStatusValuesAreBounded(t *testing.T) {
	t.Parallel()

	value := strings.Repeat("界", maximumLoginNodeStatusRunes+20)
	normalized := normalizeLoginNodeStatusValue(value)
	if len([]rune(normalized)) != maximumLoginNodeStatusRunes {
		t.Fatalf("bounded status runes = %d", len([]rune(normalized)))
	}
}
