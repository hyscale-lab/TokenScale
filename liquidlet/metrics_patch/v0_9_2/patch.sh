set -e

SCRIPT_DIR=$(cd "$(dirname "$0")"; pwd)
PATCH_FILE="$SCRIPT_DIR/combined.patch"

# Check if the patch file exists
if [ ! -f "$PATCH_FILE" ]; then
    echo "File does not exist: $PATCH_FILE"
    exit 1
fi

# Get the vLLM installation path
VLLM_PATH_OUTPUT=$(python -c "import vllm; import os; print(os.path.dirname(os.path.abspath(vllm.__file__)))" 2>/dev/null)
VLLM_PATH=$(echo "$VLLM_PATH_OUTPUT" | tail -n 1)

# Attempt a dry run of the patch to check if it's already applied
echo "🔍 Performing patch dry-run..."
if patch -p2 --dry-run -d "$VLLM_PATH" < "$PATCH_FILE"; then
    echo "✅ vLLM patch is not applied. Applying the patch now..."
    patch -p2 -d "$VLLM_PATH" < "$PATCH_FILE"
    echo "🎉 Patch applied successfully."
else
    echo "❌ Patch dry-run failed! Details:"
    # Re-run dry-run without redirecting to suppress output, to show the actual error
    patch -p2 --dry-run -d "$VLLM_PATH" < "$PATCH_FILE"
    exit 1
fi