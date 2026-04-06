package server

import (
	"context"
	"fmt"
	"net/http"

	"github.com/adrien19/noc-foundry/internal/auth"
	"github.com/go-chi/render"
)

type endpointPrincipalContextKey struct{}

func withEndpointPrincipal(ctx context.Context, principal *auth.EndpointPrincipal) context.Context {
	return context.WithValue(ctx, endpointPrincipalContextKey{}, principal)
}

func endpointPrincipalFromContext(ctx context.Context) (*auth.EndpointPrincipal, bool) {
	principal, ok := ctx.Value(endpointPrincipalContextKey{}).(*auth.EndpointPrincipal)
	return principal, ok && principal != nil
}

func currentEndpointPrincipal(ctx context.Context) *auth.EndpointPrincipal {
	principal, _ := endpointPrincipalFromContext(ctx)
	return principal
}

func resolveEndpointAuthServices(cfg ServerAuthConfig, services map[string]auth.AuthService, surface auth.EndpointSurface) (EndpointAuthPolicyConfig, []auth.EndpointAuthService) {
	policy, ok := cfg.EndpointPolicy(surface)
	if !ok || !policy.Enabled {
		return EndpointAuthPolicyConfig{}, nil
	}

	resolved := make([]auth.EndpointAuthService, 0, len(policy.AuthServices))
	for _, name := range policy.AuthServices {
		svc, ok := services[name]
		if !ok {
			continue
		}
		endpointSvc, ok := svc.(auth.EndpointAuthService)
		if !ok {
			continue
		}
		resolved = append(resolved, endpointSvc)
	}
	return policy, resolved
}

func endpointAuthMiddleware(s *Server, surface auth.EndpointSurface) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			policy, endpointServices := resolveEndpointAuthServices(s.authConfig, s.ResourceMgr.GetAuthServiceMap(), surface)
			if r.Method == http.MethodOptions || !policy.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			authz := r.Header.Get("Authorization")
			if authz == "" {
				writeEndpointAuthChallenge(w, r, surface, http.StatusUnauthorized, fmt.Errorf("missing bearer token"))
				return
			}

			for _, endpointSvc := range endpointServices {
				principal, err := endpointSvc.ValidateEndpointToken(r.Context(), r.Header, policy.Audience)
				if err != nil {
					writeEndpointAuthChallenge(w, r, surface, http.StatusUnauthorized, err)
					return
				}
				if principal == nil {
					continue
				}

				next.ServeHTTP(w, r.WithContext(withEndpointPrincipal(r.Context(), principal)))
				return
			}

			writeEndpointAuthChallenge(w, r, surface, http.StatusUnauthorized, fmt.Errorf("invalid bearer token"))
		})
	}
}

func writeEndpointAuthChallenge(w http.ResponseWriter, r *http.Request, surface auth.EndpointSurface, status int, err error) {
	w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+resourceMetadataURL(r, surface)+`"`)
	_ = render.Render(w, r, newErrResponse(err, status))
}

func sameEndpointPrincipal(a, b *auth.EndpointPrincipal) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.AuthService == b.AuthService && a.Issuer == b.Issuer && a.Subject == b.Subject
}
