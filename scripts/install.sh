#!/bin/bash
# bosun installer script
# Usage: curl -fsSL https://raw.githubusercontent.com/cameronsjo/bosun/main/scripts/install.sh | bash

set -euo pipefail

REPO="cameronsjo/bosun"
BINARY_NAME="bosun"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

info() {
    printf "${BLUE}==>${NC} %s\n" "$1"
}

success() {
    printf "${GREEN}==>${NC} %s\n" "$1"
}

warn() {
    printf "${YELLOW}==>${NC} %s\n" "$1"
}

error() {
    printf "${RED}==>${NC} %s\n" "$1" >&2
    exit 1
}

# Detect OS
detect_os() {
    case "$(uname -s)" in
        Linux*)  echo "linux" ;;
        Darwin*) echo "darwin" ;;
        *)       error "Unsupported operating system: $(uname -s)" ;;
    esac
}

# Detect architecture
detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)  echo "amd64" ;;
        arm64|aarch64) echo "arm64" ;;
        *)             error "Unsupported architecture: $(uname -m)" ;;
    esac
}

# Get latest release version
get_latest_version() {
    curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
        | grep '"tag_name":' \
        | sed -E 's/.*"([^"]+)".*/\1/' \
        | sed 's/^v//'
}

# Download and install
install_bosun() {
    local os arch version download_url archive_name tmp_dir

    os=$(detect_os)
    arch=$(detect_arch)

    info "Detected OS: ${os}, Arch: ${arch}"

    # Get version (use VERSION env var or fetch latest)
    if [ -n "${VERSION:-}" ]; then
        version="${VERSION}"
        info "Installing specified version: ${version}"
    else
        info "Fetching latest version..."
        version=$(get_latest_version)
        if [ -z "$version" ]; then
            error "Failed to determine latest version"
        fi
        info "Latest version: ${version}"
    fi

    # Construct download URL
    archive_name="${BINARY_NAME}_${version}_${os}_${arch}.tar.gz"
    download_url="https://github.com/${REPO}/releases/download/v${version}/${archive_name}"

    info "Downloading ${archive_name}..."

    # Create temp directory
    tmp_dir=$(mktemp -d)
    trap "rm -rf ${tmp_dir}" EXIT

    # Download archive
    if ! curl -fsSL "$download_url" -o "${tmp_dir}/${archive_name}"; then
        error "Failed to download ${download_url}"
    fi

    # Download checksums and verify
    checksums_url="https://github.com/${REPO}/releases/download/v${version}/checksums.txt"
    if curl -fsSL "$checksums_url" -o "${tmp_dir}/checksums.txt" 2>/dev/null; then
        info "Verifying checksum..."
        cd "${tmp_dir}"
        if command -v sha256sum &> /dev/null; then
            grep "${archive_name}" checksums.txt | sha256sum -c - > /dev/null 2>&1 || warn "Checksum verification failed"
        elif command -v shasum &> /dev/null; then
            grep "${archive_name}" checksums.txt | shasum -a 256 -c - > /dev/null 2>&1 || warn "Checksum verification failed"
        else
            warn "No checksum tool available, skipping verification"
        fi
    else
        warn "Checksums file not available, skipping verification"
    fi

    # Extract archive
    info "Extracting..."
    tar -xzf "${tmp_dir}/${archive_name}" -C "${tmp_dir}"

    # Install binary
    info "Installing to ${INSTALL_DIR}..."

    # Check if we can write to install dir
    if [ -w "${INSTALL_DIR}" ]; then
        mv "${tmp_dir}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
    else
        warn "Permission denied for ${INSTALL_DIR}, trying with sudo..."
        sudo mv "${tmp_dir}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
    fi

    # Make executable
    chmod +x "${INSTALL_DIR}/${BINARY_NAME}"

    success "Successfully installed ${BINARY_NAME} v${version} to ${INSTALL_DIR}/${BINARY_NAME}"

    # Verify installation
    if command -v "${BINARY_NAME}" &> /dev/null; then
        success "Verified: $(${BINARY_NAME} --version)"
    else
        warn "${INSTALL_DIR} may not be in your PATH"
        echo ""
        echo "Add to your shell profile:"
        echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
    fi

    # Shell completion hint
    echo ""
    info "To enable shell completions, run:"
    echo "  ${BINARY_NAME} completion --help"
}

# Main
main() {
    echo ""
    echo "  ____                        "
    echo " |  _ \\                       "
    echo " | |_) | ___  ___ _   _ _ __  "
    echo " |  _ < / _ \\/ __| | | | '_ \\ "
    echo " | |_) | (_) \\__ \\ |_| | | | |"
    echo " |____/ \\___/|___/\\__,_|_| |_|"
    echo ""
    echo " Helm for home - GitOps for Docker Compose"
    echo ""

    install_bosun
}

main "$@"
