#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OPTIONAL_FAILURES=()

log() {
  printf '[base-setup] %s\n' "$*"
}

on_error() {
  log "error at line $1 running: $2"
}

trap 'on_error "$LINENO" "$BASH_COMMAND"' ERR

retry_cmd() {
  local attempts="$1"
  local delay=2
  local try=1
  shift

  while true; do
    if "$@"; then
      return 0
    fi

    if [ "$try" -ge "$attempts" ]; then
      return 1
    fi

    log "attempt $try failed, retrying in ${delay}s"
    sleep "$delay"
    try=$((try + 1))
    delay=$((delay * 2))
  done
}

run_required() {
  local name="$1"
  shift

  log "required: $name"
  "$@"
}

run_optional() {
  local name="$1"
  shift

  log "optional: $name"
  if "$@"; then
    return 0
  fi

  local code="$?"
  OPTIONAL_FAILURES+=("$name (exit $code)")
  log "optional step failed: $name"
  return 0
}

append_if_missing() {
  local line="$1"
  local file="$2"

  if ! grep -qxF "$line" "$file" 2>/dev/null; then
    printf '%s\n' "$line" >> "$file"
  fi
}

ensure_runtime_path() {
  local path_value="$PATH"
  local path_entry

  for path_entry in "$HOME/.opencode/bin" "$HOME/.local/bin" "$HOME/bin"; do
    case ":$path_value:" in
      *":$path_entry:"*) ;;
      *) path_value="$path_entry:$path_value" ;;
    esac
  done

  export PATH="$path_value"
}

install_opencode() {
  if command -v opencode >/dev/null 2>&1; then
    log "opencode already installed"
    return 0
  fi

  retry_cmd 3 bash -c 'curl -fsSL https://opencode.ai/install | bash'
}

install_copilot() {
  if command -v copilot >/dev/null 2>&1; then
    log "copilot already installed"
    return 0
  fi

  retry_cmd 3 bash -c 'curl -fsSL https://gh.io/copilot-install | bash'
}

install_cursor_agent() {
  if command -v agent >/dev/null 2>&1 || command -v cursor >/dev/null 2>&1; then
    log "cursor agent already installed"
    return 0
  fi

  retry_cmd 3 bash -c 'curl -fsSL https://cursor.com/install | bash'
}

install_claude() {
  if command -v claude >/dev/null 2>&1; then
    log "claude already installed"
    return 0
  fi

  retry_cmd 3 bash -c 'curl -fsSL https://claude.ai/install.sh | bash'
}

install_codex() {
  if command -v codex >/dev/null 2>&1; then
    log "codex already installed"
    codex --version || true
    return 0
  fi

  if ! command -v npm >/dev/null 2>&1; then
    log "npm is not available; cannot install codex"
    return 1
  fi

  npm i -g @openai/codex
  codex --version >/dev/null
}

print_summary() {
  local tool

  log "setup verification"
  for tool in opencode codex copilot claude agent cursor; do
    if command -v "$tool" >/dev/null 2>&1; then
      log "found: $tool"
    fi
  done

  if [ "${#OPTIONAL_FAILURES[@]}" -gt 0 ]; then
    log "optional failures:"
    for failure in "${OPTIONAL_FAILURES[@]}"; do
      log "- $failure"
    done
  fi
}

export DEBIAN_FRONTEND=noninteractive
export APT_LISTCHANGES_FRONTEND=none

# base dependencies
run_required "apt update" sudo DEBIAN_FRONTEND=noninteractive apt-get update
run_required "apt dependencies" sudo DEBIAN_FRONTEND=noninteractive apt-get install -y ripgrep jq curl fzf git make build-essential xclip xsel wl-clipboard zsh

# oh-my-zsh (non-interactive install)
if [ ! -d "$HOME/.oh-my-zsh" ]; then
  RUNZSH=no CHSH=no KEEP_ZSHRC=yes \
    sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)"
fi

# set default shell to zsh for vscode user
if command -v zsh >/dev/null 2>&1; then
  if command -v chsh >/dev/null 2>&1; then
    sudo chsh -s /usr/bin/zsh vscode
  elif command -v usermod >/dev/null 2>&1; then
    sudo usermod --shell /usr/bin/zsh vscode
  else
    echo "warning: cannot set default shell to zsh" >&2
  fi
fi

# clipboard bridge (OSC 52 fallback for headless containers)
sudo tee /usr/local/bin/osc52-copy >/dev/null <<'EOF'
#!/bin/bash
set -euo pipefail

if [ -t 0 ]; then
  echo "osc52-copy: no stdin data" >&2
  exit 1
fi

payload="$(cat)"
if [ -z "$payload" ]; then
  exit 0
fi

encoded="$(printf %s "$payload" | base64 | tr -d '\n')"
sequence=$'\e]52;c;'"$encoded"$'\a'

if [ -n "${SSH_TTY:-}" ] && [ -w "${SSH_TTY:-}" ]; then
  printf %s "$sequence" > "$SSH_TTY"
elif [ -w /dev/tty ]; then
  printf %s "$sequence" > /dev/tty
else
  printf %s "$sequence"
fi
EOF
sudo chmod +x /usr/local/bin/osc52-copy

sudo tee /usr/local/bin/xclip >/dev/null <<'EOF'
#!/bin/bash
set -euo pipefail

if [ -n "${DISPLAY:-}" ] && [ -x /usr/bin/xclip ]; then
  exec /usr/bin/xclip "$@"
fi

case " $* " in
  *" -o "*|*" --output "*|*" --paste "*|*" -p "*)
    echo "xclip: paste not supported without X11/Wayland" >&2
    exit 1
    ;;
esac

if [ -t 0 ]; then
  echo "xclip: no stdin data" >&2
  exit 1
fi

exec /usr/local/bin/osc52-copy
EOF
sudo chmod +x /usr/local/bin/xclip

sudo tee /usr/local/bin/xsel >/dev/null <<'EOF'
#!/bin/bash
set -euo pipefail

if [ -n "${DISPLAY:-}" ] && [ -x /usr/bin/xsel ]; then
  exec /usr/bin/xsel "$@"
fi

case " $* " in
  *" -o "*|*" --output "*|*" --paste "*|*" -p "*)
    echo "xsel: paste not supported without X11/Wayland" >&2
    exit 1
    ;;
esac

if [ -t 0 ]; then
  echo "xsel: no stdin data" >&2
  exit 1
fi

exec /usr/local/bin/osc52-copy
EOF
sudo chmod +x /usr/local/bin/xsel

sudo tee /usr/local/bin/wl-copy >/dev/null <<'EOF'
#!/bin/bash
set -euo pipefail

if [ -n "${WAYLAND_DISPLAY:-}" ] && [ -x /usr/bin/wl-copy ]; then
  exec /usr/bin/wl-copy "$@"
fi

case " $* " in
  *" --paste "*|*" -p "*)
    echo "wl-copy: paste not supported without Wayland" >&2
    exit 1
    ;;
esac

if [ -t 0 ]; then
  echo "wl-copy: no stdin data" >&2
  exit 1
fi

exec /usr/local/bin/osc52-copy
EOF
sudo chmod +x /usr/local/bin/wl-copy

# nvm + node
if [ ! -d "$HOME/.nvm" ]; then
  run_required "install nvm" bash -c 'curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.4/install.sh | bash'
fi

export NVM_DIR="$HOME/.nvm"
[ -s "$NVM_DIR/nvm.sh" ] && \. "$NVM_DIR/nvm.sh"
nvm install node
nvm use node

# tree-sitter-cli
npm install -g tree-sitter-cli

# neovim (latest from github)
curl -LO https://github.com/neovim/neovim/releases/latest/download/nvim-linux-x86_64.tar.gz
sudo rm -rf /opt/nvim-linux-x86_64
sudo tar -C /opt -xzf nvim-linux-x86_64.tar.gz
rm nvim-linux-x86_64.tar.gz
sudo ln -sf /opt/nvim-linux-x86_64/bin/nvim /usr/local/bin/nvim

# zsh config (declarative)
if [ -f "$SCRIPT_DIR/.zshrc" ]; then
  install -m 0644 "$SCRIPT_DIR/.zshrc" ~/.zshrc
else
  echo "warning: $SCRIPT_DIR/.zshrc not found; skipping zshrc install" >&2
fi

append_if_missing 'export EDITOR=nvim' ~/.bashrc
append_if_missing 'alias v=nvim' ~/.bashrc
append_if_missing 'export PATH="$HOME/.opencode/bin:$HOME/.local/bin:$HOME/bin:$PATH"' ~/.bashrc

run_required "dotfiles" bash "$SCRIPT_DIR/dotfiles.sh"

ensure_runtime_path

run_optional "opencode install" install_opencode
run_optional "copilot install" install_copilot
run_optional "cursor agent install" install_cursor_agent
run_optional "claude install" install_claude
run_required "codex install" install_codex

print_summary
log "base setup complete"
