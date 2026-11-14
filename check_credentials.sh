#!/bin/bash
set -euo pipefail

# ============================================================
# Hardcoded Credentials Checker (improved version)
# Focuses on real secret patterns instead of any long string
# ============================================================

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo -e "${BLUE}=========================================${NC}"
echo -e "${BLUE}🔍 Hardcoded Credentials Checker${NC}"
echo -e "${BLUE}=========================================${NC}"
echo -e "📂 Project: $PROJECT_ROOT"
echo -e "⚠️  Ignoring: .env, node_modules, .git, vendor, dist, build, binaries and images"
echo ""

# ------------------------------------------------------------
# High-value patterns (low false-positive rate)
# ------------------------------------------------------------
PATTERNS=(
    # Connection strings containing username:password
    'mysql://[^:\s]+:[^@\s]+@'
    'postgres(ql)?://[^:\s]+:[^@\s]+@'
    'mongodb(\+srv)?://[^:\s]+:[^@\s]+@'
    'redis://[^:\s]+:[^@\s]+@'
    'amqp://[^:\s]+:[^@\s]+@'

    # Explicit password / secret / token assignments
    '(password|passwd|pwd|senha)\s*[:=]\s*["'\''][^"'\'']{6,}["'\'']'
    '(secret|api[_-]?key|access[_-]?key|private[_-]?key|auth[_-]?token|bearer[_-]?token)\s*[:=]\s*["'\''][^"'\'']{8,}["'\'']'
    '(token|jwt)\s*[:=]\s*["'\''][A-Za-z0-9_\-\.]{20,}["'\'']'

    # AWS keys
    'AKIA[0-9A-Z]{16}'
    'ASIA[0-9A-Z]{16}'

    # Private keys
    '-----BEGIN (RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----'

    # Full JWT (header.payload.signature)
    'eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}'

    # Suspicious default values in Getenv / process.env
    'os\.Getenv\([^)]+\)\s*[!=]=\s*["'\''][^"'\'']{8,}["'\'']'
    'process\.env\.[A-Z0-9_]+\s*[!=]=\s*["'\''][^"'\'']{8,}["'\'']'
)

# File extensions to scan
FILE_EXTENSIONS=(
    "*.go" "*.js" "*.ts" "*.tsx" "*.jsx"
    "*.py" "*.sh" "*.bash" "*.env*"
    "*.yml" "*.yaml" "*.json" "*.toml"
    "*.xml" "*.sql" "*.md" "*.properties"
)

# Directories and files to ignore
IGNORE_GLOBS=(
    "*/.git/*"
    "*/node_modules/*"
    "*/vendor/*"
    "*/dist/*"
    "*/build/*"
    "*/bin/*"
    "*/tmp/*"
    "*/.env"
    "*/.env.local"
    "*/.env.*.local"
    "*.exe" "*.dll" "*.so" "*.dylib"
    "*.png" "*.jpg" "*.jpeg" "*.gif" "*.ico" "*.svg"
    "*.log" "*.zip" "*.tar" "*.gz" "*.pdf"
)

# ------------------------------------------------------------
# Search function (uses rg if available, otherwise find + grep)
# ------------------------------------------------------------
search_files() {
    local pattern="$1"

    if command -v rg >/dev/null 2>&1; then
        # ripgrep is much faster and has better exclusion support
        rg -n -i --no-heading \
            --glob '!{.git,node_modules,vendor,dist,build,bin,tmp}/**' \
            --glob '!.env' \
            --glob '!.env.*' \
            -g '*.go' -g '*.js' -g '*.ts' -g '*.tsx' -g '*.jsx' \
            -g '*.py' -g '*.sh' -g '*.bash' \
            -g '*.yml' -g '*.yaml' -g '*.json' -g '*.toml' \
            -g '*.xml' -g '*.sql' -g '*.md' -g '*.properties' \
            -e "$pattern" "$PROJECT_ROOT" 2>/dev/null || true
    else
        # Fallback using find + grep
        local find_args=("$PROJECT_ROOT" -type f)
        local name_args=()
        for ext in "${FILE_EXTENSIONS[@]}"; do
            name_args+=(-name "$ext" -o)
        done
        # remove the trailing -o
        unset 'name_args[${#name_args[@]}-1]'

        find "${find_args[@]}" \( "${name_args[@]}" \) \
            ! -path '*/.git/*' \
            ! -path '*/node_modules/*' \
            ! -path '*/vendor/*' \
            ! -path '*/dist/*' \
            ! -path '*/build/*' \
            ! -path '*/bin/*' \
            ! -path '*/tmp/*' \
            ! -name '.env' \
            ! -name '.env.*' \
            -print0 2>/dev/null | \
        while IFS= read -r -d '' file; do
            grep -n -i -E "$pattern" "$file" 2>/dev/null | \
            while IFS= read -r line; do
                echo "${file#$PROJECT_ROOT/}:$line"
            done
        done
    fi
}

# ------------------------------------------------------------
# Main execution
# ------------------------------------------------------------
echo -e "${YELLOW}🔎 Scanning files...${NC}"
echo ""

declare -A SEEN_LINES   # prevents counting the same line multiple times
ISSUES=0
CURRENT_FILE=""

for pattern in "${PATTERNS[@]}"; do
    while IFS= read -r match; do
        [ -z "$match" ] && continue

        # Format: path:line:content
        file="${match%%:*}"
        rest="${match#*:}"
        lineno="${rest%%:*}"
        content="${rest#*:}"

        key="${file}:${lineno}"
        if [[ -n "${SEEN_LINES[$key]+x}" ]]; then
            continue
        fi
        SEEN_LINES[$key]=1

        if [[ "$file" != "$CURRENT_FILE" ]]; then
            CURRENT_FILE="$file"
            echo -e "${RED}⚠️  POSSIBLE CREDENTIAL IN:${NC} ${CYAN}$file${NC}"
        fi

        echo -e "   Line ${YELLOW}$lineno${NC}: $content"
        ISSUES=$((ISSUES + 1))
    done < <(search_files "$pattern")
done

echo ""
echo -e "${BLUE}=========================================${NC}"

if [ $ISSUES -eq 0 ]; then
    echo -e "${GREEN}✅ No hardcoded credentials found!${NC}"
    exit 0
else
    echo -e "${RED}❌ Total of $ISSUES possible hardcoded credentials found!${NC}"
    echo -e "${YELLOW}💡 Use environment variables (e.g. os.Getenv(\"DB_PASSWORD\")) instead of fixed values.${NC}"
    echo -e "${YELLOW}   The .env file at the project root remains the only allowed exception.${NC}"
    exit 1
fi