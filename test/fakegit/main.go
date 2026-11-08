//go:build unix

// fakegit is a hermetic Git and hook executable used by consumer tests and benchmarks.
// It never consults Git configuration or makes network requests.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
)

type record struct {
	Argv0        string                 `json:"argv0"`
	Argv         []string               `json:"argv"`
	Env          []string               `json:"env"`
	Cwd          string                 `json:"cwd"`
	StdinBase64  string                 `json:"stdinBase64"`
	StdinSHA256  string                 `json:"stdinSHA256"`
	StdinLength  int                    `json:"stdinLength"`
	PID          int                    `json:"pid"`
	RequestFiles map[string]requestFile `json:"requestFiles,omitempty"`
}

type requestFile struct {
	Content string `json:"content"`
	Mode    uint32 `json:"mode"`
}

func main() {
	if os.Getenv("FAKEGIT_MODE") == "pty" {
		writeRecord(terminal())
		return
	}
	if value := os.Getenv("FAKEGIT_CATCH_TERM_EXIT"); value != "" {
		code, err := strconv.Atoi(value)
		must(err)
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTERM)
		go func() { <-signals; os.Exit(code) }()
	}
	ready(os.Getenv("FAKEGIT_STARTED"))
	input, err := io.ReadAll(os.Stdin)
	must(err)
	if os.Getenv("FAKEGIT_MODE") == "hook" {
		if path := os.Getenv("FAKEGIT_HOOK_RECORD"); path != "" {
			must(os.WriteFile(path, input, 0600))
		}
		if path := os.Getenv("FAKEGIT_HOOK_ENV_RECORD"); path != "" {
			data, err := json.Marshal(os.Environ())
			must(err)
			must(os.WriteFile(path, data, 0600))
		}
		ready(os.Getenv("FAKEGIT_HOOK_READY"))
		pause(os.Getenv("FAKEGIT_HOOK_SLEEP"))
		if response, ok := os.LookupEnv("FAKEGIT_HOOK_RESPONSE"); ok {
			_, err = io.WriteString(os.Stdout, response)
		} else {
			decision := os.Getenv("FAKEGIT_HOOK_DECISION")
			if decision == "" {
				decision = "abstain"
			}
			err = json.NewEncoder(os.Stdout).Encode(map[string]string{"decision": decision, "message": os.Getenv("FAKEGIT_HOOK_MESSAGE")})
		}
		must(err)
		emit(os.Stderr, "FAKEGIT_HOOK_STDERR_B64")
		os.Exit(integer("FAKEGIT_HOOK_EXIT"))
	}
	writeRecord(input)
	ready(os.Getenv("FAKEGIT_READY"))
	if os.Getenv("FAKEGIT_OUTPUT_BEFORE_SLEEP") == "1" {
		emit(os.Stdout, "FAKEGIT_STDOUT_B64")
		emit(os.Stderr, "FAKEGIT_STDERR_B64")
	}
	pause(os.Getenv("FAKEGIT_SLEEP"))
	if os.Getenv("FAKEGIT_OUTPUT_BEFORE_SLEEP") != "1" {
		emit(os.Stdout, "FAKEGIT_STDOUT_B64")
		emit(os.Stderr, "FAKEGIT_STDERR_B64")
	}
	if sig := integer("FAKEGIT_SIGNAL"); sig != 0 {
		signal.Reset(syscall.Signal(sig))
		must(syscall.Kill(os.Getpid(), syscall.Signal(sig)))
		for {
			time.Sleep(time.Hour)
		}
	}
	os.Exit(integer("FAKEGIT_EXIT"))
}

func writeRecord(input []byte) {
	cwd, err := os.Getwd()
	must(err)
	sum := sha256.Sum256(input)
	if path := os.Getenv("FAKEGIT_RECORD"); path != "" {
		body, err := json.Marshal(record{Argv0: os.Args[0], Argv: os.Args[1:], Env: os.Environ(), Cwd: cwd, StdinBase64: base64.StdEncoding.EncodeToString(input), StdinSHA256: hex.EncodeToString(sum[:]), StdinLength: len(input), PID: os.Getpid(), RequestFiles: readRequestFiles()})
		must(err)
		file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		must(err)
		_, err = file.Write(append(body, '\n'))
		must(err)
		must(file.Close())
	}
}

func readRequestFiles() map[string]requestFile {
	if os.Getenv("FAKEGIT_READ_REQUEST") != "1" {
		return nil
	}
	files := make(map[string]requestFile)
	for i := 1; i < len(os.Args); i++ {
		flag, value, attached := strings.Cut(os.Args[i], "=")
		switch flag {
		case "--file", "-F":
			if !attached {
				i++
				if i == len(os.Args) {
					continue
				}
				value = os.Args[i]
			}
		default:
			continue
		}
		if flag == "-F" || flag == "--field" {
			if _, fieldValue, ok := strings.Cut(value, "=@"); ok {
				value = fieldValue
			} else if flag == "--field" || strings.Contains(value, "=") {
				continue
			}
		}
		if value == "-" {
			continue
		}
		data, err := os.ReadFile(value)
		must(err)
		info, err := os.Stat(value)
		must(err)
		files[value] = requestFile{Content: string(data), Mode: uint32(info.Mode().Perm())}
	}
	return files
}

func emit(w io.Writer, key string) {
	if path := os.Getenv(key[:len(key)-len("_B64")] + "_FILE"); path != "" {
		file, err := os.Open(path)
		must(err)
		_, err = io.Copy(w, file)
		must(err)
		must(file.Close())
		return
	}
	data, err := base64.StdEncoding.DecodeString(os.Getenv(key))
	must(err)
	_, err = w.Write(data)
	must(err)
}

func ready(path string) {
	if path != "" {
		must(os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0600))
	}
}

func pause(value string) {
	if value == "" {
		return
	}
	duration, err := time.ParseDuration(value)
	must(err)
	time.Sleep(duration)
}

func integer(key string) int {
	value := os.Getenv(key)
	if value == "" {
		return 0
	}
	number, err := strconv.Atoi(value)
	must(err)
	return number
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakegit:", err)
		os.Exit(125)
	}
}

// terminal exposes terminal properties and reports every resize. On the first
// interrupt it waits briefly before reporting the total, detecting duplicate
// forwarding of a terminal-generated SIGINT without a race with wrapper exit.
func terminal() []byte {
	signals := make(chan os.Signal, 16)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGWINCH)
	defer signal.Stop(signals)
	report := func(event string, count int) {
		size, err := pty.GetsizeFull(os.Stdin)
		_, outErr := pty.GetsizeFull(os.Stdout)
		_, errErr := pty.GetsizeFull(os.Stderr)
		value := map[string]any{"event": event, "stdinTTY": err == nil, "stdoutTTY": outErr == nil, "stderrTTY": errErr == nil, "interrupts": count}
		if size != nil {
			value["rows"], value["cols"] = size.Rows, size.Cols
		}
		must(json.NewEncoder(os.Stdout).Encode(value))
	}
	report("ready", 0)
	ready(os.Getenv("FAKEGIT_READY"))
	input := make(chan []byte, 1)
	go func() {
		defer close(input)
		reader := bufio.NewReader(os.Stdin)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				input <- line
			}
			if err != nil {
				return
			}
		}
	}()
	var received []byte
	var timer <-chan time.Time
	count := 0
	for {
		select {
		case line, ok := <-input:
			if !ok {
				input = nil
				continue
			}
			received = append(received, line...)
			must(json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "input", "input": string(line)}))
		case sig := <-signals:
			if sig == syscall.SIGWINCH {
				report("resize", count)
				continue
			}
			count++
			if timer == nil {
				timer = time.After(300 * time.Millisecond)
			}
		case <-timer:
			report("interrupt", count)
			return received
		}
	}
}
