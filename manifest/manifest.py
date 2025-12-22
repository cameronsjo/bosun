#!/usr/bin/env python3
"""Crew Manifest - Generate compose, traefik, and gatus configs from service manifests."""

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


def load_provision(provision_name: str, variables: dict, provisions_dir: Path) -> dict:
    """Load provision, interpolate variables, parse YAML."""
    provision_path = provisions_dir / f"{provision_name}.yml"
    if not provision_path.exists():
        raise FileNotFoundError(f"Provision not found: {provision_path}")

    raw_content = provision_path.read_text()
    interpolated = interpolate(raw_content, variables)
    return yaml.safe_load(interpolated) or {}


def render_service(manifest: dict, provisions_dir: Path) -> dict[str, dict]:
    """Render a service manifest into compose/traefik/gatus outputs."""
    name = manifest["name"]
    config = manifest.get("config", {})
    provisions = manifest.get("provisions", [])

    # Build variables from config + name
    variables = {"name": name, **config}

    # Initialize output accumulators
    outputs = {"compose": {}, "traefik": {}, "gatus": {}}

    # Handle raw passthrough mode
    if manifest.get("type") == "raw":
        if "compose" in manifest:
            outputs["compose"] = {"services": manifest["compose"]}
        return outputs

    # Load and merge provisions
    for provision_name in provisions:
        provision = load_provision(provision_name, variables, provisions_dir)
        for target in ("compose", "traefik", "gatus"):
            if target in provision:
                outputs[target] = deep_merge(outputs[target], provision[target])

    # Handle sidecar services (postgres, redis, etc.)
    sidecars = manifest.get("services", {})
    for sidecar_type, sidecar_config in sidecars.items():
        sidecar_vars = {"name": name, "sidecar": sidecar_type, **sidecar_config, **config}
        provision = load_provision(sidecar_type, sidecar_vars, provisions_dir)
        for target in ("compose", "traefik", "gatus"):
            if target in provision:
                outputs[target] = deep_merge(outputs[target], provision[target])

    return outputs


def render_stack(stack_path: Path, provisions_dir: Path, services_dir: Path) -> dict[str, dict]:
    """Render a stack file into compose/traefik/gatus outputs."""
    stack = yaml.safe_load(stack_path.read_text())
    outputs = {"compose": {}, "traefik": {}, "gatus": {}}

    for service_file in stack.get("include", []):
        service_path = services_dir / service_file
        if not service_path.exists():
            raise FileNotFoundError(f"Service not found: {service_path}")

        manifest = yaml.safe_load(service_path.read_text())
        service_outputs = render_service(manifest, provisions_dir)

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
    # Manifest dir is parent of stacks/ or services/
    if "stacks" in str(input_path) or "services" in str(input_path):
        manifest_dir = input_path.parent.parent
    else:
        manifest_dir = input_path.parent
    provisions_dir = manifest_dir / "provisions"
    services_dir = manifest_dir / "services"
    output_dir = manifest_dir / "output"

    if not input_path.exists():
        print(f"Error: {input_path} not found", file=sys.stderr)
        return 1

    # Determine if stack or single service
    if "stacks" in str(input_path):
        outputs = render_stack(input_path, provisions_dir, services_dir)
        stack_name = input_path.stem
    else:
        manifest = yaml.safe_load(input_path.read_text())
        outputs = render_service(manifest, provisions_dir)
        stack_name = manifest["name"]

    if args.dry_run:
        print(yaml.dump(outputs, default_flow_style=False, sort_keys=False))
    else:
        write_outputs(outputs, output_dir, stack_name)

    return 0


def cmd_provisions(args: argparse.Namespace) -> int:
    """List available provisions."""
    provisions_dir = Path(args.dir) / "provisions"
    if not provisions_dir.exists():
        print(f"Error: {provisions_dir} not found", file=sys.stderr)
        return 1

    for provision in sorted(provisions_dir.glob("*.yml")):
        print(f"  - {provision.stem}")
    return 0


def cmd_expand(args: argparse.Namespace) -> int:
    """Show what a service manifest expands to."""
    input_path = Path(args.path)
    provisions_dir = input_path.parent.parent / "provisions"

    manifest = yaml.safe_load(input_path.read_text())
    outputs = render_service(manifest, provisions_dir)
    print(yaml.dump(outputs, default_flow_style=False, sort_keys=False))
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description="Crew Manifest - provision your services")
    subparsers = parser.add_subparsers(dest="command", required=True)

    # render command
    render_parser = subparsers.add_parser("render", help="Render a stack or service")
    render_parser.add_argument("path", help="Path to stack or service manifest")
    render_parser.add_argument("--dry-run", action="store_true", help="Print output without writing")

    # provisions command
    provisions_parser = subparsers.add_parser("provisions", help="List available provisions")
    provisions_parser.add_argument("--dir", default=".", help="Manifest directory")

    # expand command
    expand_parser = subparsers.add_parser("expand", help="Show expanded service")
    expand_parser.add_argument("path", help="Path to service manifest")

    args = parser.parse_args()

    commands = {"render": cmd_render, "provisions": cmd_provisions, "expand": cmd_expand}

    return commands[args.command](args)


if __name__ == "__main__":
    sys.exit(main())
