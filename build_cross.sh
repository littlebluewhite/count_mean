#!/bin/bash

# Get Go bin path
GOBIN=$(go env GOBIN)
if [ -z "$GOBIN" ]; then
    GOPATH=$(go env GOPATH)
    GOBIN="$GOPATH/bin"
fi

# Install fyne-cross if not already installed
echo "Checking for fyne-cross installation..."
if [ ! -f "$GOBIN/fyne-cross" ]; then
    echo "Installing fyne-cross..."
    go install github.com/fyne-io/fyne-cross@latest
fi

# Check if Docker is running (required for fyne-cross)
if ! docker info >/dev/null 2>&1; then
    echo "Error: Docker is not running. Please start Docker Desktop and try again."
    echo "fyne-cross requires Docker to build cross-platform binaries."
    exit 1
fi

# Build for Windows
echo "Building for Windows (amd64)..."
"$GOBIN/fyne-cross" windows -arch amd64 -output emg_gui_tool.exe .

# Build for Linux
echo "Building for Linux (amd64)..."
"$GOBIN/fyne-cross" linux -arch amd64 -output emg_gui_tool_linux .

# Build for macOS (current platform)
echo "Building for macOS..."
go build -o emg_gui_tool_macos main.go

echo "Build complete! Check the fyne-cross/dist folder for Windows and Linux builds."