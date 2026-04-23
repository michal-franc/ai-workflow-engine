#!/usr/bin/env bash
set -euo pipefail

# Downloads all issues from a GitHub Project and converts them to markdown
# files with YAML frontmatter, organized by System field.
#
# Usage: ./sync-issues.sh <owner> <project-number> [output-dir]
#   owner          — GitHub username or org
#   project-number — GitHub project number
#   output-dir     — defaults to ./issues

if [ $# -lt 2 ]; then
    echo "Usage: $0 <owner> <project-number> [output-dir]"
    echo "Example: $0 my-username 4 ./issues"
    exit 1
fi

OWNER="$1"
PROJECT_NUMBER="$2"
OUTPUT_DIR="${3:-./issues}"

echo "Syncing issues from github.com/users/$OWNER/projects/$PROJECT_NUMBER"
echo "Output directory: $OUTPUT_DIR"

# Clean output dir
rm -rf "$OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"

echo "Fetching all project items..."
ITEMS=$(gh project item-list "$PROJECT_NUMBER" \
    --owner "$OWNER" \
    --format json \
    --limit 500 \
    | jq '.items')

COUNT=$(echo "$ITEMS" | jq 'length')
echo "Found $COUNT items"

echo "$ITEMS" | jq -c '.[]' | while read -r ITEM; do
    TITLE=$(echo "$ITEM" | jq -r '.title // ""')
    STATUS=$(echo "$ITEM" | jq -r '.status // ""')
    SYSTEM=$(echo "$ITEM" | jq -r '.system // ""')
    VERSION=$(echo "$ITEM" | jq -r '.version // ""')
    BODY=$(echo "$ITEM" | jq -r '.content.body // ""')
    NUMBER=$(echo "$ITEM" | jq -r '.content.number // ""')
    REPO=$(echo "$ITEM" | jq -r '.content.repository // ""')

    # Labels as YAML array
    LABELS=$(echo "$ITEM" | jq -r '
        if (.labels | length) > 0 then
            .labels | map("  - " + .) | join("\n")
        else
            ""
        end
    ')

    # Determine system directory (default to _unsorted)
    SYS_DIR="$SYSTEM"
    if [ -z "$SYS_DIR" ]; then
        SYS_DIR="_unsorted"
    fi
    mkdir -p "$OUTPUT_DIR/$SYS_DIR"

    # Build filename from number or sanitized title
    if [ -n "$NUMBER" ] && [ "$NUMBER" != "null" ]; then
        FILENAME="${NUMBER}.md"
    else
        FILENAME=$(echo "$TITLE" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/-/g' | sed 's/--*/-/g' | sed 's/^-//' | sed 's/-$//')
        FILENAME="${FILENAME}.md"
    fi

    # Write markdown file
    {
        echo "---"
        echo "title: \"$(echo "$TITLE" | sed 's/"/\\"/g')\""
        echo "status: \"$STATUS\""
        if [ -n "$SYSTEM" ]; then
            echo "system: \"$SYSTEM\""
        fi
        if [ -n "$VERSION" ] && [ "$VERSION" != "null" ]; then
            echo "version: \"$VERSION\""
        fi
        if [ -n "$LABELS" ]; then
            echo "labels:"
            echo "$LABELS"
        fi
        if [ -n "$NUMBER" ] && [ "$NUMBER" != "null" ]; then
            echo "number: $NUMBER"
        fi
        if [ -n "$REPO" ] && [ "$REPO" != "null" ]; then
            echo "repo: \"$REPO\""
        fi
        echo "---"
        echo ""
        if [ -n "$BODY" ] && [ "$BODY" != "null" ]; then
            echo "$BODY"
        fi
    } > "$OUTPUT_DIR/$SYS_DIR/$FILENAME"
done

echo ""
echo "Done! Synced to $OUTPUT_DIR:"
find "$OUTPUT_DIR" -type d | sort | while read -r dir; do
    count=$(find "$dir" -maxdepth 1 -name "*.md" 2>/dev/null | wc -l)
    if [ "$count" -gt 0 ]; then
        echo "  $(basename "$dir")/: $count issues"
    fi
done
