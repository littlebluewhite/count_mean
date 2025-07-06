# Note: Direct cross-compilation for GUI apps requires fyne-cross
# Install with: go install github.com/fyne-io/fyne-cross@latest

# Build for current platform
build:
	go build -o emg_gui_tool main.go

# Build for macOS
build-macos:
	go build -o emg_gui_tool_macos main.go

# Build for Windows using fyne-cross (requires Docker)
build-windows:
	@echo "Building for Windows requires fyne-cross and Docker"
	@echo "Install fyne-cross: go install github.com/fyne-io/fyne-cross@latest"
	@echo "Then run: fyne-cross windows -arch amd64 -output emg_gui_tool.exe ."

# Build for Linux using fyne-cross (requires Docker)
build-linux:
	@echo "Building for Linux requires fyne-cross and Docker"
	@echo "Install fyne-cross: go install github.com/fyne-io/fyne-cross@latest"
	@echo "Then run: fyne-cross linux -arch amd64 -output emg_gui_tool_linux ."

# Build all platforms using the build script
build-all:
	./build_cross.sh

# Old targets (kept for compatibility)
w-build:
	@echo "Direct cross-compilation not supported for GUI apps."
	@echo "Please use 'make build-windows' for instructions."

u-build:
	go build -o main main.go