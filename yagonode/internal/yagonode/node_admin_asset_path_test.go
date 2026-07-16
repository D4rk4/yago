package yagonode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

func TestGuardAdminSurfaceRejectsAdminAssetAliasesBeforeOperationsMux(t *testing.T) {
	t.Parallel()

	service, err := provisionAdminAuth(
		context.Background(),
		nodeConfig{Admin: adminConfig{Username: "admin", Password: "pw"}},
		openTestVault(t),
		nil,
	)
	if err != nil {
		t.Fatalf("provision admin auth: %v", err)
	}
	opsMux := testOpsMux()
	opsMux.Handle(adminui.BasePath, adminui.New(adminui.Options{}))
	surface := guardAdminSurface(service, opsMux)

	tests := []struct {
		name         string
		target       string
		cacheControl string
	}{
		{name: "asset dot segment", target: "/admin/assets/./carbon.css", cacheControl: "no-store"},
		{
			name:         "asset repeated separator",
			target:       "/admin//assets/carbon.css",
			cacheControl: "no-store",
		},
		{
			name:         "asset encoded separator",
			target:       "/admin%2fassets/carbon.css",
			cacheControl: "no-store",
		},
		{
			name:         "stylesheet dot segment",
			target:       "/admin/./auth.css",
			cacheControl: "private, no-store",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequestWithContext(
				t.Context(),
				http.MethodGet,
				test.target,
				nil,
			)
			surface.ServeHTTP(response, request)
			if response.Code != http.StatusNotFound || response.Header().Get("Location") != "" ||
				response.Header().Get("Cache-Control") != test.cacheControl {
				t.Fatalf("%s = %d %v", test.target, response.Code, response.Header())
			}
		})
	}
}
