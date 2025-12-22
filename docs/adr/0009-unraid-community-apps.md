# ADR-0009: Unraid Community Apps Registration

## Status

Evaluating

## Context

Unraid is a primary target platform for unops. The [Community Apps (CA)](https://unraid.net/community/apps) plugin is the standard way Unraid users discover and install Docker containers.

**Question:** Should we register unops components with Unraid Community Apps?

## Components to Consider

| Component | CA Candidate? | Notes |
|-----------|---------------|-------|
| **Conductor** | Yes | Core orchestrator, primary value prop |
| **Tailscale Gateway** | No | Official Tailscale template exists |
| **Agentgateway** | Maybe | Niche (MCP users only) |
| **Composer** | No | CLI tool, not a container |

## Registration Requirements

Based on [Unraid CA documentation](https://docs.unraid.net/unraid-os/using-unraid-to/run-docker-containers/community-applications/):

### 1. Docker Image on Registry
```
ghcr.io/cameronsjo/unops-conductor:latest
```
- Must be public
- Must have versioned tags (not just `latest`)
- Recommended: Multi-arch (amd64 + arm64)

### 2. XML Template
```xml
<?xml version="1.0"?>
<Container version="2">
  <Name>unops-conductor</Name>
  <Repository>ghcr.io/cameronsjo/unops-conductor</Repository>
  <Registry>https://github.com/cameronsjo/unops/pkgs/container/unops-conductor</Registry>
  <Branch>
    <Tag>latest</Tag>
    <TagDescription>Latest stable release</TagDescription>
  </Branch>
  <Network>bridge</Network>
  <Privileged>false</Privileged>
  <Support>https://forums.unraid.net/topic/XXXXX-unops-conductor/</Support>
  <Project>https://github.com/cameronsjo/unops</Project>
  <Overview>
    GitOps for Docker Compose on bare metal. Push to GitHub, your server updates.
    Encrypted secrets with SOPS, templated configs with Chezmoi, instant webhook deploys.
  </Overview>
  <Category>Tools: Productivity:</Category>
  <Icon>https://raw.githubusercontent.com/cameronsjo/unops/main/assets/icon.png</Icon>
  <ExtraParams>--restart=unless-stopped</ExtraParams>
  <PostArgs/>
  <DonateText/>
  <DonateLink/>
  <DonateImg/>

  <Config Name="Config Path" Target="/config" Default="/mnt/user/appdata/unops" Mode="rw" Description="Configuration directory" Type="Path" Display="always" Required="true" Mask="false"/>
  <Config Name="Repo URL" Target="REPO_URL" Default="" Mode="" Description="Git repository URL for your configs" Type="Variable" Display="always" Required="true" Mask="false"/>
  <Config Name="Webhook Port" Target="8080" Default="8080" Mode="tcp" Description="Webhook listener port" Type="Port" Display="always" Required="true" Mask="false"/>
  <Config Name="Age Key File" Target="SOPS_AGE_KEY_FILE" Default="/config/age-key.txt" Mode="" Description="Path to Age private key" Type="Variable" Display="always" Required="true" Mask="false"/>
  <Config Name="Docker Socket" Target="/var/run/docker.sock" Default="/var/run/docker.sock" Mode="ro" Description="Docker socket (read-only)" Type="Path" Display="advanced" Required="true" Mask="false"/>
</Container>
```

### 3. Support Thread
- Create thread in [Unraid Forums](https://forums.unraid.net/)
- Category: Docker Containers
- Title: `[Support] unops-conductor - GitOps for Docker Compose`
- Must be actively monitored

### 4. Template Repository
- GitHub repo with XML templates
- Example: `cameronsjo/unraid-templates`
- Structure:
  ```
  unraid-templates/
  ├── README.md
  └── templates/
      └── unops-conductor.xml
  ```

### 5. Submit to Squid
- Contact Squid271 (CA maintainer)
- Provide link to template repo
- Link to support thread

## Alternative: Self-Hosted Templates

Skip CA registration, host templates ourselves:

```
Users add template repo URL manually:
https://github.com/cameronsjo/unops/tree/main/unraid-templates
```

**Pros:**
- No approval process
- Full control
- Faster updates

**Cons:**
- Less discoverability
- Users must add repo manually
- No CA search results

## Decision Matrix

| Factor | CA Registration | Self-Hosted |
|--------|-----------------|-------------|
| Discoverability | High | Low |
| Approval process | Required | None |
| Update speed | Depends on CA | Instant |
| User trust | Higher (vetted) | Lower |
| Maintenance | Support thread required | GitHub issues |
| Time investment | Medium | Low |

## Recommendation

**Phase 1: Self-hosted templates**
- Ship XML templates in unops repo
- Document manual installation
- Test with early adopters
- Iterate on template based on feedback

**Phase 2: CA registration (when stable)**
- Create support thread
- Submit to CA after v1.0
- Benefit from discoverability

## Implementation Tasks

### Phase 1 Checklist

- [ ] Create `unraid-templates/` directory in repo
- [ ] Write conductor XML template
- [ ] Create icon (512x512 PNG)
- [ ] Add installation docs for manual template repo
- [ ] Test on fresh Unraid install

### Phase 2 Checklist

- [ ] Create Unraid forums account
- [ ] Create support thread
- [ ] Contact Squid271
- [ ] Update XML with support thread URL
- [ ] Submit for CA inclusion

## Template Design Considerations

### Docker Socket Access
Conductor needs Docker socket to run `docker compose`. Options:

1. **Direct mount (simpler, less secure)**
   ```xml
   <Config Name="Docker Socket" Target="/var/run/docker.sock" Default="/var/run/docker.sock" Mode="ro" Description="Docker socket" Type="Path" Display="advanced" Required="true" Mask="false"/>
   ```

2. **Via socket proxy (more secure)**
   ```xml
   <Config Name="Docker Host" Target="DOCKER_HOST" Default="tcp://dockersocket:2375" Mode="" Description="Docker socket proxy" Type="Variable" Display="advanced" Required="true" Mask="false"/>
   ```
   Requires separate dockersocket container.

### Compose Manager Integration

Unraid's Docker Compose Manager plugin stores projects in:
```
/boot/config/plugins/compose.manager/projects/
```

Conductor should be aware of this path for Unraid-specific deployments.

### Network Considerations

Conductor may need access to:
- `proxynet` (if using Traefik)
- `mcp-net` (if using Agentgateway)
- Host network (for webhook from Tailscale)

Template should expose network configuration.

## References

- [Unraid Community Apps](https://unraid.net/community/apps)
- [CA Documentation](https://docs.unraid.net/unraid-os/using-unraid-to/run-docker-containers/community-applications/)
- [Template Request Repo](https://github.com/selfhosters/unRAID-CA-templates)
- [How to Publish Templates](https://forums.unraid.net/topic/101424-how-to-publish-docker-templates-to-community-applications-on-unraid/)
- [Submitting an Unraid Community App](https://gennari.com/posts/submitting-unraid-community-app/)
