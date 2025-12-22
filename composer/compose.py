#!/usr/bin/env python3
"""Service Composer - Generate compose, traefik, and gatus configs from service manifests."""

import argparse
import re
import sys
from copy import deepcopy
from pathlib import Path
from typing import Any

import yaml

# Keys that use set-union merge instead of list-replace
UNION_KEYS = {"networks", "depends_on"}
# Keys that extend lists instead of replacing
EXTEND_KEYS = {"endpoints"}


def interpolate(template: str, variables: dict[str, Any]) -> str:
    """Replace ${var} placeholders with values. Raises on missing variables."""
    pattern = re.compile(r"\$\{(\w+)\}")

    def replacer(match: re.Match) -> str:
        key = match.group(1)
        if key not in variables:
            raise ValueError(f"Missing variable: ${{{key}}}")
        return str(variables[key])

    return pattern.sub(replacer, template)


def normalize_to_dict(value: Any, key_name: str) -> dict[str, str]:
    """Convert list-style environment/labels to dict format."""
    if value is None:
        return {}
    if isinstance(value, dict):
        return {str(k): str(v) for k, v in value.items()}
    if isinstance(value, list):
        result = {}
        for item in value:
            if "=" in str(item):
                k, v = str(item).split("=", 1)
                result[k] = v
            else:
                raise ValueError(f"Invalid {key_name} format: {item}")
        return result
    raise ValueError(f"Invalid {key_name} type: {type(value)}")


def deep_merge(base: dict, overlay: dict, path: str = "") -> dict:
    """Recursively merge overlay into base. Returns new dict."""
    result = deepcopy(base)

    for key, value in overlay.items():
        current_path = f"{path}.{key}" if path else key

        if key not in result:
            result[key] = deepcopy(value)
        elif isinstance(result[key], dict) and isinstance(value, dict):
            # Normalize environment/labels before dict merge
            if key in ("environment", "labels"):
                result[key] = normalize_to_dict(result[key], key)
                value = normalize_to_dict(value, key)
            result[key] = deep_merge(result[key], value, current_path)
        elif isinstance(result[key], list) and isinstance(value, list):
            # Set union for networks/depends_on, extend for endpoints, replace for others
            if key in UNION_KEYS:
                result[key] = list(set(result[key]) | set(value))
            elif key in EXTEND_KEYS:
                result[key] = result[key] + deepcopy(value)
            else:
                result[key] = deepcopy(value)
        else:
            result[key] = deepcopy(value)

    return result


def load_profile(profile_name: str, variables: dict, profiles_dir: Path) -> dict:
    """Load profile, interpolate variables, parse YAML."""
    profile_path = profiles_dir / f"{profile_name}.yml"
    if not profile_path.exists():
        raise FileNotFoundError(f"Profile not found: {profile_path}")

    raw_content = profile_path.read_text()
    interpolated = interpolate(raw_content, variables)
    return yaml.safe_load(interpolated) or {}


def render_service(manifest: dict, profiles_dir: Path) -> dict[str, dict]:
    """Render a service manifest into compose/traefik/gatus outputs."""
    name = manifest["name"]
    config = manifest.get("config", {})
    profiles = manifest.get("profiles", [])

    # Build variables from config + name
    variables = {"name": name, **config}

    # Initialize output accumulators
    outputs = {"compose": {}, "traefik": {}, "gatus": {}}

    # Handle raw passthrough mode
    if manifest.get("type") == "raw":
        if "compose" in manifest:
            outputs["compose"] = {"services": manifest["compose"]}
        return outputs

    # Load and merge profiles
    for profile_name in profiles:
        profile = load_profile(profile_name, variables, profiles_dir)
        for target in ("compose", "traefik", "gatus"):
            if target in profile:
                outputs[target] = deep_merge(outputs[target], profile[target])

    # Handle sidecar services (postgres, redis, etc.)
    sidecars = manifest.get("services", {})
    for sidecar_type, sidecar_config in sidecars.items():
        sidecar_vars = {"name": name, "sidecar": sidecar_type, **sidecar_config, **config}
        profile = load_profile(sidecar_type, sidecar_vars, profiles_dir)
        for target in ("compose", "traefik", "gatus"):
            if target in profile:
                outputs[target] = deep_merge(outputs[target], profile[target])

    return outputs


def render_stack(stack_path: Path, profiles_dir: Path, services_dir: Path) -> dict[str, dict]:
    """Render a stack file into compose/traefik/gatus outputs."""
    stack = yaml.safe_load(stack_path.read_text())
    outputs = {"compose": {}, "traefik": {}, "gatus": {}}

    for service_file in stack.get("include", []):
        service_path = services_dir / service_file
        if not service_path.exists():
            raise FileNotFoundError(f"Service not found: {service_path}")

        manifest = yaml.safe_load(service_path.read_text())
        service_outputs = render_service(manifest, profiles_dir)

        for target in ("compose", "traefik", "gatus"):
            outputs[target] = deep_merge(outputs[target], service_outputs[target])

    # Add network definitions from stack
    if "networks" in stack:
        outputs["compose"]["networks"] = stack["networks"]

    return outputs


def write_outputs(outputs: dict[str, dict], output_dir: Path, stack_name: str) -> None:
    """Write rendered outputs to files."""
    output_dir.mkdir(parents=True, exist_ok=True)

    for target, content in outputs.items():
        if not content:
            continue

        target_dir = output_dir / target
        target_dir.mkdir(exist_ok=True)

        filename = f"{stack_name}.yml" if target == "compose" else "dynamic.yml"
        if target == "gatus":
            filename = "endpoints.yml"

        output_path = target_dir / filename
        output_path.write_text(yaml.dump(content, default_flow_style=False, sort_keys=False))
        print(f"Wrote: {output_path}")


def cmd_render(args: argparse.Namespace) -> int:
    """Render a stack or service manifest."""
    input_path = Path(args.path)
    # Composer dir is parent of stacks/ or services/
    if "stacks" in str(input_path) or "services" in str(input_path):
        composer_dir = input_path.parent.parent
    else:
        composer_dir = input_path.parent
    profiles_dir = composer_dir / "profiles"
    services_dir = composer_dir / "services"
    output_dir = composer_dir / "output"

    if not input_path.exists():
        print(f"Error: {input_path} not found", file=sys.stderr)
        return 1

    # Determine if stack or single service
    if "stacks" in str(input_path):
        outputs = render_stack(input_path, profiles_dir, services_dir)
        stack_name = input_path.stem
    else:
        manifest = yaml.safe_load(input_path.read_text())
        outputs = render_service(manifest, profiles_dir)
        stack_name = manifest["name"]

    if args.dry_run:
        print(yaml.dump(outputs, default_flow_style=False, sort_keys=False))
    else:
        write_outputs(outputs, output_dir, stack_name)

    return 0


def cmd_profiles(args: argparse.Namespace) -> int:
    """List available profiles."""
    profiles_dir = Path(args.dir) / "profiles"
    if not profiles_dir.exists():
        print(f"Error: {profiles_dir} not found", file=sys.stderr)
        return 1

    for profile in sorted(profiles_dir.glob("*.yml")):
        print(f"  - {profile.stem}")
    return 0


def cmd_expand(args: argparse.Namespace) -> int:
    """Show what a service manifest expands to."""
    input_path = Path(args.path)
    profiles_dir = input_path.parent.parent / "profiles"

    manifest = yaml.safe_load(input_path.read_text())
    outputs = render_service(manifest, profiles_dir)
    print(yaml.dump(outputs, default_flow_style=False, sort_keys=False))
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description="Service Composer")
    subparsers = parser.add_subparsers(dest="command", required=True)

    # render command
    render_parser = subparsers.add_parser("render", help="Render a stack or service")
    render_parser.add_argument("path", help="Path to stack or service manifest")
    render_parser.add_argument("--dry-run", action="store_true", help="Print output without writing")

    # profiles command
    profiles_parser = subparsers.add_parser("profiles", help="List available profiles")
    profiles_parser.add_argument("--dir", default=".", help="Composer directory")

    # expand command
    expand_parser = subparsers.add_parser("expand", help="Show expanded service")
    expand_parser.add_argument("path", help="Path to service manifest")

    args = parser.parse_args()

    commands = {"render": cmd_render, "profiles": cmd_profiles, "expand": cmd_expand}

    return commands[args.command](args)


if __name__ == "__main__":
    sys.exit(main())
