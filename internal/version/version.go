package version

import (
	"fmt"
)

var Version = "source"

func String() string {
	return fmt.Sprintf("sprout version: %s\n", Version)
}
