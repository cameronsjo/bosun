# Troubleshooting Guide

## Common Issues

### "project root not found"

Bosun searches upward for `bosun/` or `manifest/` directory.

- Ensure you're inside a bosun project
- Or specify path: `bosun --root /path/to/project`

### "connect to docker: ..."

- Check Docker is running: `docker ps`
- Check Docker socket permissions
- On Linux: `sudo usermod -aG docker $USER`

### "sops decrypt failed"

- Verify SOPS is installed: `sops --version`
- Check age key exists: `ls ~/.config/sops/age/keys.txt`
- Set key path: `export SOPS_AGE_KEY_FILE=/path/to/key`

### "docker compose: command not found"

Bosun requires Docker Compose v2:

- Install: https://docs.docker.com/compose/install/
- Verify: `docker compose version`

### SSH connection failures

- Test manually: `ssh user@host exit`
- Check SSH key is loaded: `ssh-add -l`
- Verify host is reachable: `ping host`

## Debug Mode

Set verbose output:

```bash
bosun --verbose provision mystack
```

## Getting Help

- GitHub Issues: https://github.com/cameronsjo/bosun/issues
- Run diagnostics: `bosun doctor`
