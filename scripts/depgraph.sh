#!/usr/bin/env bash
# Generate a DOT dependency graph of btidy-internal package imports,
# then render it to SVG via Graphviz.
#
# Usage: scripts/depgraph.sh [output.svg]
# Dependencies: go, dot (graphviz)

set -euo pipefail

OUTPUT="${1:-docs/deps.svg}"
MODULE="btidy"

# Verify graphviz is installed.
if ! command -v dot >/dev/null 2>&1; then
    echo "error: graphviz not found (apt install graphviz)" >&2
    exit 1
fi

# Build DOT graph from go list output.
# Uses -f template to extract only ImportPath and Imports, avoiding JSON parsing issues.
# Output format: one line per package, "ImportPath|import1,import2,..."
raw=$(go list -f '{{.ImportPath}}|{{join .Imports ","}}' ./... 2>/dev/null)

# Collect all internal package paths.
declare -A internal
while IFS='|' read -r pkg _; do
    if [[ "$pkg" == "$MODULE"* ]]; then
        internal["$pkg"]=1
    fi
done <<< "$raw"

# Helper: strip module prefix for short labels.
label() {
    local path="$1"
    if [[ "$path" == "$MODULE/"* ]]; then
        echo "${path#"$MODULE/"}"
    else
        echo "$path"
    fi
}

# Build DOT output.
{
    echo 'digraph deps {'
    echo '    rankdir=LR;'
    echo '    node [shape=box, fontname="Helvetica", fontsize=10];'
    echo '    edge [color="#666666"];'
    echo ''

    while IFS='|' read -r pkg imports; do
        if [[ -z "${internal[$pkg]+x}" ]]; then
            continue
        fi
        src=$(label "$pkg")
        IFS=',' read -ra deps <<< "$imports"
        for dep in "${deps[@]}"; do
            if [[ -n "${internal[$dep]+x}" && "$dep" != "$pkg" ]]; then
                dst=$(label "$dep")
                echo "    \"$src\" -> \"$dst\";"
            fi
        done
    done <<< "$raw"

    echo '}'
} > /tmp/btidy-deps.dot

# Ensure output directory exists.
mkdir -p "$(dirname "$OUTPUT")"

# Render to SVG.
dot -Tsvg -o "$OUTPUT" /tmp/btidy-deps.dot
rm -f /tmp/btidy-deps.dot
echo "dependency graph written to $OUTPUT"
