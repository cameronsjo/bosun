// Package manifest provides YAML rendering for service definitions.
//
// It transforms service manifests into docker-compose, traefik, and gatus
// configuration files. The rendering pipeline includes:
//
//   - Variable interpolation (${var} syntax)
//   - Deep merging with union/extend/replace semantics
//   - Provision inheritance via includes
//   - Sidecar expansion with sensible defaults
//
// # Manifest Structure
//
// A service manifest defines a deployable service:
//
//	name: myapp
//	compose:
//	  services:
//	    myapp:
//	      image: myapp:latest
//	traefik:
//	  entrypoints: [https]
//
// # Provisions
//
// Provisions are reusable templates that can be included:
//
//	includes:
//	  - webapp  # loads provisions/webapp.yml
//	values:
//	  domain: example.com
package manifest
