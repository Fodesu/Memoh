#!/usr/bin/env sh
set -e

if [ "$(uname -s)" = "Darwin" ]; then
  limactl start default
  limactl shell default -- sudo containerd --version
  exit $?
fi

if command -v containerd >/dev/null 2>&1 && command -v nerdctl >/dev/null 2>&1 && command -v buildctl >/dev/null 2>&1 && command -v buildkitd >/dev/null 2>&1; then
  containerd --version
  nerdctl --version
  buildctl --version
  exit 0
fi

if ! command -v containerd >/dev/null 2>&1; then
  if command -v apt-get >/dev/null 2>&1; then
    sudo apt-get update
    sudo apt-get install -y containerd
  elif command -v dnf >/dev/null 2>&1; then
    sudo dnf install -y containerd
  elif command -v yum >/dev/null 2>&1; then
    sudo yum install -y containerd
  elif command -v apk >/dev/null 2>&1; then
    sudo apk add --no-cache containerd
  else
    echo "No supported package manager found. Install containerd manually."
    exit 1
  fi
fi

if ! command -v nerdctl >/dev/null 2>&1 || ! command -v buildctl >/dev/null 2>&1 || ! command -v buildkitd >/dev/null 2>&1; then
  OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
  ARCH="$(uname -m)"
  NERDCTL_VERSION="${NERDCTL_VERSION:-}"

  if [ "$OS" != "linux" ]; then
    echo "Automatic nerdctl installation from release is only supported on Linux."
    exit 1
  fi

  case "$ARCH" in
    x86_64|amd64)
      ARCH="amd64"
      ;;
    aarch64|arm64)
      ARCH="arm64"
      ;;
    *)
      echo "Unsupported architecture for nerdctl release: $ARCH"
      exit 1
      ;;
  esac

  if [ -z "$NERDCTL_VERSION" ]; then
    RELEASES_API_URL="https://api.github.com/repos/containerd/nerdctl/releases/latest"
    if command -v curl >/dev/null 2>&1; then
      NERDCTL_VERSION="$(curl -fsSL "$RELEASES_API_URL" | sed -n 's/.*"tag_name":[[:space:]]*"v\{0,1\}\([^"]*\)".*/\1/p' | head -n1)"
    elif command -v wget >/dev/null 2>&1; then
      NERDCTL_VERSION="$(wget -qO- "$RELEASES_API_URL" | sed -n 's/.*"tag_name":[[:space:]]*"v\{0,1\}\([^"]*\)".*/\1/p' | head -n1)"
    fi
  fi

  if [ -z "$NERDCTL_VERSION" ]; then
    echo "Failed to detect latest nerdctl version. Set NERDCTL_VERSION manually."
    exit 1
  fi

  NERDCTL_TARBALL="nerdctl-full-${NERDCTL_VERSION}-linux-${ARCH}.tar.gz"
  NERDCTL_URL="https://github.com/containerd/nerdctl/releases/download/v${NERDCTL_VERSION}/${NERDCTL_TARBALL}"
  TMP_DIR="$(mktemp -d)"
  TMP_TARBALL="${TMP_DIR}/${NERDCTL_TARBALL}"

  cleanup() {
    rm -rf "$TMP_DIR"
  }
  trap cleanup EXIT INT TERM

  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$NERDCTL_URL" -o "$TMP_TARBALL"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$TMP_TARBALL" "$NERDCTL_URL"
  else
    echo "curl or wget is required to download nerdctl."
    exit 1
  fi

  tar -xzf "$TMP_TARBALL" -C "$TMP_DIR"
  sudo install -m 0755 "$TMP_DIR/bin/nerdctl" /usr/local/bin/nerdctl
  sudo install -m 0755 "$TMP_DIR/bin/buildctl" /usr/local/bin/buildctl
  sudo install -m 0755 "$TMP_DIR/bin/buildkitd" /usr/local/bin/buildkitd

  if command -v systemctl >/dev/null 2>&1 && [ -f "$TMP_DIR/lib/systemd/system/buildkit.service" ]; then
    sudo install -m 0644 "$TMP_DIR/lib/systemd/system/buildkit.service" /etc/systemd/system/buildkit.service
    sudo systemctl daemon-reload
    sudo systemctl enable --now buildkit.service || true
  fi
fi

containerd --version
nerdctl --version
buildctl --version
