# Example Homelab GitOps Repository

This is a simplified example of a homelab infrastructure repository managed by Bosun.

## Structure

```
homelab/
├── .sops.yaml                    # SOPS encryption config
├── secrets.sops.yaml             # Encrypted secrets (use secrets.example.yaml as reference)
├── secrets.example.yaml          # Example secrets structure (not encrypted)
├── unraid/
│   ├── compose/
│   │   └── core.yml.tmpl         # Docker Compose stack template
│   └── appdata/
│       ├── traefik/
│       │   └── conf.d/
│       │       └── dynamic.yml.tmpl
│       ├── authelia/
│       │   └── configuration.yml.tmpl
│       ├── gatus/
│       │   └── config.yaml.tmpl
│       └── tailscale-gateway/
│           └── serve.json
└── README.md
```

## Quick Start

1. **Fork this example** and make it private (contains your secrets)

2. **Generate an age key** for SOPS encryption:
   ```bash
   age-keygen -o ~/.config/sops/age/keys.txt
   ```

3. **Update `.sops.yaml`** with your age public key:
   ```yaml
   creation_rules:
     - path_regex: secrets\.yaml$
       age: age1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
   ```

4. **Create your secrets file**:
   ```bash
   cp secrets.example.yaml secrets.yaml
   # Edit secrets.yaml with your values
   sops --encrypt --in-place secrets.yaml
   mv secrets.yaml secrets.sops.yaml
   ```

5. **Deploy Bosun** on your server:
   ```bash
   # Copy age key to server
   scp ~/.config/sops/age/keys.txt user@server:/path/to/age-key.txt

   # Create bosun docker-compose.yml (see docs/deployment.md)
   ```

6. **Add GitHub webhook** pointing to your Bosun instance

7. **Push changes** - Bosun auto-deploys!

## Template Syntax

Templates use Go templates with [sprig functions](http://masterminds.github.io/sprig/).

Access secrets via the template context:
```yaml
{{- $secrets := . -}}
password: {{ $secrets.database.password }}
api_key: {{ $secrets.services.api_key }}
```

## Local Preview

```bash
# Render templates locally before pushing
SOPS_AGE_KEY_FILE=~/.config/sops/age/keys.txt \
  bosun render -s secrets.sops.yaml unraid/compose/core.yml.tmpl
```

## Customization

This example uses:
- **Traefik** - Reverse proxy with automatic HTTPS
- **Authelia** - SSO/OIDC provider
- **Gatus** - Uptime monitoring
- **Tailscale** - Secure remote access via Funnel

Adapt the templates to your stack. Bosun works with any Docker Compose setup.
