// fsnotify-probe: minimal test to verify fsnotify on Windows for plugin.yaml
// in subdirectory.
//
// Run:
//   go run ./cmd/fsnotify-probe -dir testdata\fsnotify-probe -log F:\tmp\probe.log
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

var (
	logMu sync.Mutex
	logF  *os.File
)

func logln(s string) {
	logMu.Lock()
	defer logMu.Unlock()
	ts := time.Now().Format("15:04:05.000")
	line := fmt.Sprintf("[%s] %s\n", ts, s)
	if logF != nil {
		logF.WriteString(line)
		logF.Sync()
	}
	fmt.Print(line)
}

func main() {
	dir := flag.String("dir", "testdata/fsnotify-probe", "plugin root dir to watch")
	logPath := flag.String("log", "F:/tmp/probe.log", "log file path (line-buffered fsync)")
	flag.Parse()

	if *logPath != "" {
		f, err := os.Create(*logPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "create log:", err)
			os.Exit(1)
		}
		logF = f
		defer f.Close()
	}

	if err := os.MkdirAll(filepath.Join(*dir, "foo"), 0o755); err != nil {
		logln("mkdir error: " + err.Error())
		os.Exit(1)
	}
	yamlPath := filepath.Join(*dir, "foo", "plugin.yaml")
	if _, err := os.Stat(yamlPath); os.IsNotExist(err) {
		if err := os.WriteFile(yamlPath, []byte("# initial\n"), 0o644); err != nil {
			logln("write initial: " + err.Error())
			os.Exit(1)
		}
		logln("wrote initial " + yamlPath)
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		logln("new watcher: " + err.Error())
		os.Exit(1)
	}
	defer w.Close()

	if err := w.Add(*dir); err != nil {
		logln("watch root: " + err.Error())
		os.Exit(1)
	}
	if err := w.Add(filepath.Join(*dir, "foo")); err != nil {
		logln("watch subdir: " + err.Error())
		os.Exit(1)
	}
	logln(fmt.Sprintf("watching %s and %s", *dir, filepath.Join(*dir, "foo")))

	timeout := time.After(30 * time.Second)

	for {
		select {
		case <-timeout:
			logln("done (timeout)")
			return
		case ev, ok := <-w.Events:
			if !ok {
				logln("events channel closed")
				return
			}
			logln(fmt.Sprintf("op=%s name=%q base=%s", ev.Op.String(), ev.Name, filepath.Base(ev.Name)))
		case err, ok := <-w.Errors:
			if !ok {
				logln("errors channel closed")
				return
			}
			logln("ERROR: " + err.Error())
		}
	}
}
