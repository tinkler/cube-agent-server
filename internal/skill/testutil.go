package skill

import "os"

// readFileFS helper for tests
func readFileFS(path string) ([]byte, error) {
	return os.ReadFile(path)
}
