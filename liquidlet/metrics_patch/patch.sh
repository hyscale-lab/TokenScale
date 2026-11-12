set -e

SCRIPT_DIR=$(cd "$(dirname "$0")"; pwd)
PATCH_FILE="$SCRIPT_DIR/combined.patch"

# Check if the patch file exists
if [ ! -f "$PATCH_FILE" ]; then
    echo "File does not exist: $PATCH_FILE"
    exit 1
fi

# Get the vLLM installation path
VLLM_PATH=$(python -c "import vllm; import os; print(os.path.dirname(os.path.abspath(vllm.__file__)))")

# Attempt a dry run of the patch to check if it's already applied
if patch -p2 --dry-run -d "$VLLM_PATH" < "$PATCH_FILE" > /dev/null 2>&1; then
    echo "vLLM patch is not applied. Applying the patch now..."
    # Apply the patch
    patch -p2 -d "$VLLM_PATH" < "$PATCH_FILE"
    echo "Patch applied successfully."
else
    echo "vLLM patch has already been applied. Skipping..."
fi