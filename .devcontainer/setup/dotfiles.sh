#!/bin/bash
set -euo pipefail

log_info() {
  printf '[dotfiles] %s\n' "$*"
}

log_error() {
  printf '[dotfiles] %s\n' "$*" >&2
}

if [ -z "${GITHUB_PAT_TOKEN:-}" ]; then
  echo "GITHUB_PAT_TOKEN is not set. Export it on the host before 'devpod up'." >&2
  exit 1
fi

git_no_cred() {
  GIT_TERMINAL_PROMPT=0 \
  GIT_ASKPASS= \
  SSH_ASKPASS= \
  git -c credential.helper= "$@"
}

git_auth_header() {
  printf 'AUTHORIZATION: Basic %s' \
    "$(printf 'x-access-token:%s' "$GITHUB_PAT_TOKEN" | base64 | tr -d '\n')"
}

clone_or_update() {
  local repo="$1"
  local target="$2"
  local url="https://github.com/haapjari/${repo}.git"

  mkdir -p "$(dirname "$target")"

  if [ -d "$target/.git" ]; then
    log_info "updating ${repo} -> ${target}"
    if ! git_no_cred -C "$target" \
      -c "http.extraHeader=$(git_auth_header)" \
      pull --ff-only --quiet; then
      log_error "failed to update ${repo}"
      exit 1
    fi
    log_info "updated ${repo}"
    return
  fi

  if [ -e "$target" ]; then
    log_error "target exists but is not a git repo: ${target}"
    exit 1
  fi

  log_info "cloning ${repo} -> ${target}"
  if ! git_no_cred -c "http.extraHeader=$(git_auth_header)" \
    clone --quiet "$url" "$target"; then
    log_error "failed to clone ${url}"
    exit 1
  fi
  log_info "cloned ${repo}"
}

clone_or_update opencode "$HOME/.config/opencode"
clone_or_update nvim "$HOME/.config/nvim"
clone_or_update .agents "$HOME/.agents"
clone_or_update .claude "$HOME/.claude"
clone_or_update .copilot "$HOME/.copilot"
clone_or_update .codex "$HOME/.codex"
clone_or_update .cursor "$HOME/.cursor"
