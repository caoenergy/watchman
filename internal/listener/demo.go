package listener

import (
	"fmt"
	"path/filepath"
	"strings"
)

func LoggingHandler(eventType string, eventDirectory string, eventFile string, _ bool) {
	filename := eventFile
	if (eventType == "DELETE" || eventType == "DELETE_SELF") && strings.Contains(filename, " (deleted)") {
		filename = filename[:strings.LastIndex(eventFile, " (deleted)")]
	}
	fmt.Println(filepath.Join(eventDirectory, filename))
}
