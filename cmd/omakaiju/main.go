package main

import (
	"fmt"
	"os"

	"omakaiju/internal/app"
	"omakaiju/internal/config"

	tea "charm.land/bubbletea/v2"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "omakaiju: %v\n", err)
		os.Exit(1)
	}

	m := app.NewModel(cfg)
	p := tea.NewProgram(m)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "omakaiju: %v\n", err)
		os.Exit(1)
	}
}
