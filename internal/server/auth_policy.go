package server

import (
	"fmt"
	"slices"

	"github.com/adrien19/noc-foundry/internal/auth"
)

func ValidateAndApplyEndpointAuthConfig(cfg ServerAuthConfig, authServices map[string]auth.AuthService) error {
	if err := ValidateServerAuthConfig(cfg); err != nil {
		return err
	}

	serviceAudiences := make(map[string][]string)
	for _, surface := range []auth.EndpointSurface{auth.EndpointSurfaceAPI, auth.EndpointSurfaceMCP} {
		policy, _ := cfg.EndpointPolicy(surface)
		if !policy.Enabled {
			continue
		}

		for _, name := range policy.AuthServices {
			svc, ok := authServices[name]
			if !ok {
				return fmt.Errorf("server auth endpointAuth.%s references unknown auth service %q", surface, name)
			}
			if _, ok := svc.(auth.EndpointAuthService); !ok {
				return fmt.Errorf("server auth endpointAuth.%s auth service %q does not support endpoint auth", surface, name)
			}
			if _, ok := svc.(auth.IssuerProvider); !ok {
				return fmt.Errorf("server auth endpointAuth.%s auth service %q does not expose an OIDC issuer", surface, name)
			}
			if !slices.Contains(serviceAudiences[name], policy.Audience) {
				serviceAudiences[name] = append(serviceAudiences[name], policy.Audience)
			}
		}
	}

	if cfg.UI.Enabled {
		svc, ok := authServices[cfg.UI.AuthService]
		if !ok {
			return fmt.Errorf("server auth ui.authService references unknown auth service %q", cfg.UI.AuthService)
		}
		if _, ok := svc.(auth.AuthorizationServerMetadataProvider); !ok {
			return fmt.Errorf("server auth ui.authService %q does not expose OIDC authorization server metadata", cfg.UI.AuthService)
		}
		if _, ok := svc.(auth.IssuerProvider); !ok {
			return fmt.Errorf("server auth ui.authService %q does not expose an OIDC issuer", cfg.UI.AuthService)
		}
	}

	for name, svc := range authServices {
		configurer, ok := svc.(auth.EndpointAudienceConfigurer)
		if !ok {
			continue
		}
		configurer.SetEndpointAudiences(serviceAudiences[name])
	}

	return nil
}
