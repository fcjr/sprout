package nix

import (
	"fmt"
	"sync"
)

var (
	displayLines       = make([]string, 4)
	displayIndex       = 0
	displayMutex       sync.Mutex
	displayInitialized = false
)

func displayStreamingLine(line, color string) {
	displayMutex.Lock()
	defer displayMutex.Unlock()

	if line == "" {
		return
	}

	if !displayInitialized {
		fmt.Printf("      %s%s\033[0m\n", color, "")
		fmt.Printf("      %s%s\033[0m\n", color, "")
		fmt.Printf("      %s%s\033[0m\n", color, "")
		fmt.Printf("      %s%s\033[0m\n", color, "")
		fmt.Print("\033[4A")
		displayInitialized = true
	}

	displayLines[displayIndex] = line
	displayIndex = (displayIndex + 1) % 4

	for i := 0; i < 4; i++ {
		lineIndex := (displayIndex + i) % 4
		displayLine := ""
		if displayLines[lineIndex] != "" {
			displayLine = displayLines[lineIndex]
			if len(displayLine) > 76 {
				displayLine = displayLine[:73] + "..."
			}
		}
		fmt.Printf("\033[K      %s%s\033[0m\n", color, displayLine)
	}
	fmt.Print("\033[4A")
}

func finishStreamingDisplay() {
	displayMutex.Lock()
	defer displayMutex.Unlock()

	if displayInitialized {
		fmt.Print("\033[4B")
		displayInitialized = false
		displayIndex = 0
		for i := range displayLines {
			displayLines[i] = ""
		}
	}
}
