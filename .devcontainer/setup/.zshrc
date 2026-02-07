# zsh config for devcontainer
export EDITOR=nvim
alias v=nvim
export PATH="$HOME/.opencode/bin:$HOME/.local/bin:$HOME/bin:$PATH"

# oh-my-zsh
export ZSH="$HOME/.oh-my-zsh"
ZSH_THEME="evan"

if [ -d "$ZSH" ]; then
  source "$ZSH/oh-my-zsh.sh"
else
  # history
  HISTFILE=~/.zsh_history
  HISTSIZE=10000
  SAVEHIST=10000
  setopt hist_ignore_dups
  setopt share_history

  # completion
  autoload -Uz compinit
  compinit

  # prompt
  PROMPT='%n@%m:%~%# '
fi
