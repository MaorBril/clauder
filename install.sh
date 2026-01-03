#!/bin/sh
set -e

# Clauder installer script
# Usage: curl -sSL https://raw.githubusercontent.com/MaorBril/clauder/main/install.sh | sh

REPO="MaorBril/clauder"
INSTALL_DIR="${CLAUDER_INSTALL_DIR:-$HOME/.local/bin}"

# Detect OS
detect_os() {
    case "$(uname -s)" in
        Linux*)  echo "linux" ;;
        Darwin*) echo "darwin" ;;
        MINGW*|MSYS*|CYGWIN*) echo "windows" ;;
        *) echo "unsupported" ;;
    esac
}

# Detect architecture
detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64) echo "amd64" ;;
        arm64|aarch64) echo "arm64" ;;
        *) echo "unsupported" ;;
    esac
}

# Get latest release tag
get_latest_version() {
    curl -sSL "https://api.github.com/repos/${REPO}/releases/latest" | \
        grep '"tag_name":' | \
        sed -E 's/.*"([^"]+)".*/\1/'
}

main() {
    echo "Installing clauder..."

    OS=$(detect_os)
    ARCH=$(detect_arch)

    if [ "$OS" = "unsupported" ] || [ "$ARCH" = "unsupported" ]; then
        echo "Error: Unsupported OS or architecture: $(uname -s) $(uname -m)"
        echo "Please install manually from https://github.com/${REPO}/releases"
        exit 1
    fi

    VERSION=$(get_latest_version)
    if [ -z "$VERSION" ]; then
        echo "Error: Could not determine latest version"
        exit 1
    fi

    echo "  OS: $OS"
    echo "  Arch: $ARCH"
    echo "  Version: $VERSION"

    # Build download URL
    BINARY="clauder-${OS}-${ARCH}"
    if [ "$OS" = "windows" ]; then
        BINARY="${BINARY}.exe"
    fi
    URL="https://github.com/${REPO}/releases/download/${VERSION}/${BINARY}"

    echo "  Downloading from: $URL"

    # Create install directory if it doesn't exist
    mkdir -p "$INSTALL_DIR"

    # Download binary
    DEST="${INSTALL_DIR}/clauder"
    if [ "$OS" = "windows" ]; then
        DEST="${DEST}.exe"
    fi

    if command -v curl >/dev/null 2>&1; then
        curl -sSL "$URL" -o "$DEST"
    elif command -v wget >/dev/null 2>&1; then
        wget -q "$URL" -O "$DEST"
    else
        echo "Error: curl or wget required"
        exit 1
    fi

    # Make executable
    chmod +x "$DEST"

    echo ""
    echo "Installed clauder to $DEST"

    # Check if install dir is in PATH
    case ":$PATH:" in
        *":$INSTALL_DIR:"*) ;;
        *)
            echo ""
            echo "Add clauder to your PATH by adding this to your shell profile:"
            echo ""
            echo "  export PATH=\"\$PATH:$INSTALL_DIR\""
            echo ""
            ;;
    esac

    echo "Run 'clauder setup' to configure your AI coding tool."
}

main
