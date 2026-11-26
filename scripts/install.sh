#!/bin/bash
set -euo pipefail

# bosun installer
# Usage: curl -fsSL https://raw.githubusercontent.com/cameronsjo/bosun/main/scripts/install.sh | bash

REPO="cameronsjo/bosun"
BINARY="bosun"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

info() { echo -e "${BLUE}==>${NC} $1"; }
success() { echo -e "${GREEN}==>${NC} $1"; }
warn() { echo -e "${YELLOW}==>${NC} $1"; }
error() { echo -e "${RED}==>${NC} $1" >&2; exit 1; }

# Detect OS and architecture
detect_platform() {
    local os arch

    case "$(uname -s)" in
        Linux*)  os="linux" ;;
        Darwin*) os="darwin" ;;
        *)       error "Unsupported OS: $(uname -s)" ;;
    esac

    case "$(uname -m)" in
        x86_64|amd64)  arch="amd64" ;;
        arm64|aarch64) arch="arm64" ;;
        *)             error "Unsupported architecture: $(uname -m)" ;;
    esac

    echo "${os}_${arch}"
}

# Get latest release version
get_latest_version() {
    curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" |
        grep '"tag_name":' |
        sed -E 's/.*"([^"]+)".*/\1/'
}

# Verify checksum
verify_checksum() {
    local file="$1"
    local checksums_url="$2"
    local expected

    info "Verifying checksum..."

    expected=$(curl -fsSL "$checksums_url" | grep "$(basename "$file")" | awk '{print $1}')

    if command -v sha256sum &>/dev/null; then
        actual=$(sha256sum "$file" | awk '{print $1}')
    elif command -v shasum &>/dev/null; then
        actual=$(shasum -a 256 "$file" | awk '{print $1}')
    else
        error "Neither sha256sum nor shasum found; refusing to install unverified binary"
    fi

    if [ "$expected" != "$actual" ]; then
        error "Checksum verification failed!\nExpected: $expected\nActual: $actual"
    fi

    success "Checksum verified"
}

# Main installation
main() {
    info "Installing ${BINARY}..."

    local platform version archive_name download_url checksums_url

    platform=$(detect_platform)
    info "Detected platform: ${platform}"

    version=$(get_latest_version)
    if [ -z "$version" ]; then
        error "Could not determine latest version"
    fi
    info "Latest version: ${version}"

    # Remove 'v' prefix for archive name
    local version_num="${version#v}"
    archive_name="${BINARY}_${version_num}_${platform}.tar.gz"
    download_url="https://github.com/${REPO}/releases/download/${version}/${archive_name}"
    checksums_url="https://github.com/${REPO}/releases/download/${version}/checksums.txt"

    TMP_DIR=$(mktemp -d)
    trap 'rm -rf "${TMP_DIR:-}"' EXIT

    info "Downloading ${archive_name}..."
    curl -fsSL -o "${TMP_DIR}/${archive_name}" "$download_url" ||
        error "Failed to download ${download_url}"

    verify_checksum "${TMP_DIR}/${archive_name}" "$checksums_url"

    info "Extracting..."
    tar -xzf "${TMP_DIR}/${archive_name}" -C "$TMP_DIR"

    info "Installing to ${INSTALL_DIR}..."
    if [ -w "$INSTALL_DIR" ]; then
        mv "${TMP_DIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
    else
        sudo mv "${TMP_DIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
    fi
    chmod +x "${INSTALL_DIR}/${BINARY}"

    success "Successfully installed ${BINARY} ${version} to ${INSTALL_DIR}/${BINARY}"
    echo ""
    info "Run 'bosun --help' to get started"
    info "Run 'bosun update' to update to the latest version"
}

main "$@"
