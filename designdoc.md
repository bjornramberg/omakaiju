omakaiju - TUI File Manager Design Document
omakaiju - TUI File Manager Design Document

1. Vision & Architecture
omakaiju is a modern, ultra-lightweight, dual-pane Terminal User Interface (TUI) file manager engineered for speed, extensibility, and seamless aesthetic integration. Designed primarily for Unix-like environments, it provides terminal power-users with an intuitive, key-driven workflow without sacrificing visually rich previews or systemic performance.

Implementation Language: Built natively in Go (Golang) to ensure optimal concurrency handling, low memory overhead, and lightning-fast startup times.
UI Framework Choice: Built on top of Bubbletea (The Elm Architecture for terminal apps) paired with Lipgloss for declarative layout styling, or Tview/Ccell primitives for robust widget rendering, providing determinism and state isolation across all view components.
Compilation & Distribution: Compiles to a single, statically linked binary with zero external runtime dependencies. Ideal for quick deployment across local environments, SSH sessions, and containerized workflows.
Core Architecture:
Asynchronous I/O: All filesystem reads, metadata queries, and thumbnail/preview generation routines run in non-blocking background goroutines.
Event-Driven State Engine: Key presses and system events dispatch explicit actions to update a single source of truth application state.
Resource Footprint: Target idle memory usage under 15MB with instantaneous window resizing updates.


2. Omarchy Theme Integration
To maintain absolute visual consistency across the operating environment, omakaiju features full dynamic integration with the Omarchy thematic ecosystem.

Configuration Template (fm.toml.tpl):
The application reads its visual definitions from a dynamic TOML template rendered by the local Omarchy engine.
Allows system-wide palette changes to propagate instantly down to the file manager without custom scripting.
Color Mapping Schema: Maps application UI elements directly to standardized Omarchy color variables:

UI Component
Omarchy Variable
Description / Purpose
Active Pane Border
@omarchy.primary
Highlights the focused navigation region
Inactive Pane Border
@omarchy.subtle
De-emphasizes non-focused containers
File Selection Bar
@omarchy.selection_bg
Highlight strip for the cursor line
Text Color (Standard)
@omarchy.fg_main
Primary file and directory list item text
Executable / Link Text
@omarchy.accent
Accent color for special file modes
Top/Bottom Status Bars
@omarchy.bg_dark
Contrast background for utility panels


Dynamic Theme Hot-Reloading:
Utilizes system fsnotify bindings to watch fm.toml for runtime file modifications.
When the theme configuration changes, omakaiju re-parses color rules and updates Lipgloss style models in real time without requiring an application restart.


3. UI Layout & Dynamic Windows
The interface operates on a flexible, grid-based layout tailored for context-rich file navigation and management.

Top Bar (Global Status & Path Info):
Displays current working path, hostname, active filter parameters, and system state flags.
Includes a tab line when multi-workspace mode is engaged.
Dual-Pane View (Primary Workspaces):
Left Pane / Right Pane: Symmetric directory trees operating independently.
Supports dynamic pane resizing (50/50 split default, adjustable via keybindings).
Visual indicators distinctly mark the active vs. inactive pane via border palette switching and status indicators.
Context Panel (Dynamic Right/Bottom Sidecar):
File Previews: Real-time text syntax highlighting, binary hex inspection, archive structure inspection, and image preview rendering via Kitty/Sixel graphics protocols where supported.
Metadata View: Displays extended file attributes, permissions, file size, line counts, and modified timestamps when preview is disabled.
Bottom Bar (Interactive Prompt & Command Line):
Displays hotkey cheatsheets, runtime error alerts, operation progress bars (e.g., file copy status), and input fields for quick-search/commands.
Typography & Iconography
Nerd Fonts Support: Treated as a first-class citizen throughout the application.
Nerd Font glyphs are used natively for file type icons, directory indicators, git status badges, and UI boundaries.
Provides a highly visual, modern terminal experience seamlessly integrated into the file manager.


4. Keybindings & Usability
omakaiju enforces a modal, ergonomic keyboard navigation workflow supporting both standard navigational conventions and standard Vim motion paradigms.
Navigation & Focus Control
h / Left Arrow: Navigate up to parent directory.
j / Down Arrow: Move cursor down within list.
k / Up Arrow: Move cursor up within list.
l / Right Arrow / Enter: Enter selected directory or open file with default handler.
Tab: Toggle active focus between Left and Right panes.
Ctrl+r: Force refresh directory tree and metadata cache.
Core File Operations
y (Yank / Copy): Stage selected file(s) for copy operation.
m (Move): Stage selected file(s) for move operation.
p (Paste): Execute staged copy/move operation from active pane to the target path of the inactive pane.
d (Delete): Prompt for removal (trash or force delete) of selected item(s).
a (Add): Open prompt to create a new file or directory (ending in /).
r (Rename): Inline path/filename editing.
Search & Fuzzy Finding
/ (Filter): Local directory filtering (filters the visible list instantly as you type).
Ctrl+f (Fuzzy Find): Triggers an integrated fuzzy finder overlay powered by an internal Go-native fzf algorithm over the sub-tree path.
Esc: Clear current filter, dismiss modal windows, or return to standard modal view.


