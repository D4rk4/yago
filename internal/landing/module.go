package landing

import "net/http"

type Module struct {
	Endpoint http.Handler
}

func New() Module {
	return Module{Endpoint: landingEndpoint{}}
}
