// Copyright 2016 Jürgen Hötzel
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.


package main

import (
	"github.com/robertkowalski/graylog-golang"
	"log"
	"flag"
	"encoding/json"
	"fmt"
	"os/exec"
	"os"
	"bufio"
	"strings"
)

type GelfMessage struct {
	Version string
	Level int
	ShortMessage string `json:"short_message"`
	Message string  `json:"full_message"`
	Pid int `json:"_pid"`
	Command string `json:"_command"`
	Failed int `json:"_failed,omitempty"`
}

func formatLogSplit(command string, line string, level int, failed int) []byte {
	m := GelfMessage{Version:"1.1", ShortMessage:line, Level:level, Command:command, Failed:failed}
	if b, err := json.Marshal(m); err != nil {
		log.Fatal(fmt.Sprintf("Failed to Marshal Gelf Message: %s", err))
		return []byte{}
	} else {
		return b
	}
}
func formatLog(command string, output  string, level int) []byte {
	shortMessage := "Standard Output"
	if level == 4 {
		shortMessage = "Standard Error Output"
	}
	m := GelfMessage{Version:"1.1", Level:level, ShortMessage:shortMessage,
		Message:output, Command:command}
	// omit output if message output is empty
	if b, err := json.Marshal(m); err != nil && output != "" {
		log.Fatal(fmt.Sprintf("Failed to Marshal Gelf Message: %s", err))
		return []byte{}
	} else {
		return b
	}
}

func main() {
	// command line options
	var splitLines = flag.Bool("split", false, "if true, split output on newlines")
	var host = flag.String("logserver", "localhost", "Graylog Server")
	flag.Usage = func() {
		fmt.Printf("Usage: graylog-tee [options] command [arg]...\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() < 1 {
		log.Fatal("Missing command")
	}

	// collect output if we don't split lins
	var stdoutLines []string
	var stderrLines []string

	// command to be executed
	cmd := exec.Command(flag.Args()[0], flag.Args()[1:]...)
	stderrReader, err := cmd.StderrPipe()
	if err != nil {
		log.Fatalf("Failed to create Stderr Pipe: %v", err)
	}
	stdoutReader, err2 := cmd.StdoutPipe()
	if err2 != nil {
		log.Fatalf("Failed to create Stdout Pipe: %v", err2)
	}
	cmd.Stdin = os.Stdin
	commandStr := strings.Join(cmd.Args, " ")

	// stdout/stderr channel for multiplexing
	stderr := make(chan string)
	stdout := make(chan string)
	go func() {
		scanner := bufio.NewScanner(stderrReader)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintln(os.Stderr, line)
			stderr <- line
		}
		close(stderr)
	}()
	go func() {
		scanner := bufio.NewScanner(stdoutReader)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Println(line)
			stdout <- line
		}
		close(stdout)
	}()

	// GrayLog setup
	g := gelf.New(gelf.Config{GraylogHostname: *host})

	if err := cmd.Start(); err != nil {
		message := fmt.Sprintf("Failed to start command: %v", err)
		if b := formatLogSplit(commandStr, message, 4, 1); len(b) != 0 {
			g.Send(b)
		}
		log.Fatalf(message)
	} else {
		message := fmt.Sprintf("Started command: %v", commandStr)
		if b := formatLogSplit(commandStr, message, 6, 0); len(b) != 0 {
			g.Send(b)
		}
	}
	// Multiplex reading
	for {
		select {
		case stdoutLine, ok := <-stdout:
			if !ok {
				stdout = nil
			} else {
				if *splitLines {
					if b := formatLogSplit(commandStr, stdoutLine, 6, 0); len(b) != 0 {
						g.Send(b)
					}
				} else {
					stdoutLines = append(stdoutLines, stdoutLine)
				}
			}
		case stderrLine, ok := <-stderr:
			if !ok {
				stderr = nil
			} else {
				if *splitLines {
					if b := formatLogSplit(commandStr, stderrLine, 4, 0); len(b) != 0 {
						g.Send(b)
					}
				}
				stderrLines = append(stderrLines, stderrLine)
			}
		}
		if stdout == nil &&  stderr == nil {
			break
		}
	}

	cmd.Wait()
	// oneshot output
	if !*splitLines {
		if b := formatLog(commandStr, strings.Join(stdoutLines, "\n"), 6); len(b) != 0 {
			g.Send(b)
		}
		if b := formatLog(commandStr, strings.Join(stderrLines, "\n"), 4); len(b) != 0 {
			g.Send(b)
		}
	}


	failed := !cmd.ProcessState.Success()
	// sadly there is no way to get the original exit code. On failure we just return 1
	var exitCode int
	var level int
	if !failed {
		exitCode = 0
	} else {
		exitCode = 1
	}

	finishedMessage := "Command succeeded"
	if failed {
		finishedMessage = "Command failed"
	}
	if b := formatLogSplit(commandStr, finishedMessage, level, exitCode); len(b) != 0 {
		g.Send(b)
	}
	os.Exit(exitCode)
}
