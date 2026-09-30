package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"
)

type cliOptions struct {
	JSON    bool
	Quiet   bool
	Wait    bool
	Timeout time.Duration
}

var cliCommands = map[string]bool{
	"help": true, "version": true, "doctor": true, "install-cli": true,
	"project": true, "server": true, "registry": true, "deployment": true,
	"release": true, "job": true,
}

func shouldRunCLI(arguments []string) bool {
	name := filepath.Base(os.Args[0])
	if name == "ideploy" {
		return true
	}
	if len(arguments) == 0 {
		return false
	}
	first := strings.TrimSpace(arguments[0])
	return cliCommands[first] || first == "--help" || first == "-h" || first == "--version"
}

func runCLI(arguments []string, stdout, stderr io.Writer) int {
	request, options, special, err := parseCLIArguments(arguments)
	if err != nil {
		writeCLIError(stdout, stderr, options.JSON, "usage", err.Error())
		return 2
	}
	if special == "help" {
		fmt.Fprint(stdout, cliHelp)
		return 0
	}
	if special == "install-cli" {
		status, err := installCommandLineTool()
		if err != nil {
			writeCLIError(stdout, stderr, options.JSON, "install_failed", err.Error())
			return 1
		}
		return writeCLIResult(stdout, stderr, options, cliResponseFromValue(status))
	}
	if request.Command == "version" {
		return writeCLIResult(stdout, stderr, options, executeCLIRequest(context.Background(), nil, request))
	}

	request.TimeoutMS = options.Timeout.Milliseconds()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	return writeCLIResult(stdout, stderr, options, executeRuntimeRequest(ctx, request))
}

func parseCLIArguments(arguments []string) (cliRequest, cliOptions, string, error) {
	options := cliOptions{Wait: true, Timeout: 30 * time.Minute}
	filtered := make([]string, 0, len(arguments))
	helpRequested := false
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch {
		case argument == "--json":
			options.JSON = true
		case argument == "--quiet":
			options.Quiet = true
		case argument == "--wait":
			options.Wait = true
		case argument == "--no-wait":
			options.Wait = false
		case argument == "--no-color":
			// Output currently uses no ANSI colour. Keep the option stable for callers.
		case argument == "--help" || argument == "-h":
			helpRequested = true
		case argument == "--version" && index == 0:
			filtered = append(filtered, "version")
		case argument == "--timeout":
			if index+1 >= len(arguments) {
				return cliRequest{}, options, "", errors.New("--timeout requires a duration")
			}
			index++
			duration, err := time.ParseDuration(arguments[index])
			if err != nil || duration <= 0 || duration > 24*time.Hour {
				return cliRequest{}, options, "", errors.New("--timeout must be a positive duration no greater than 24h")
			}
			options.Timeout = duration
		case strings.HasPrefix(argument, "--timeout="):
			duration, err := time.ParseDuration(strings.TrimPrefix(argument, "--timeout="))
			if err != nil || duration <= 0 || duration > 24*time.Hour {
				return cliRequest{}, options, "", errors.New("--timeout must be a positive duration no greater than 24h")
			}
			options.Timeout = duration
		default:
			filtered = append(filtered, argument)
		}
	}
	if helpRequested {
		return cliRequest{}, options, "help", nil
	}
	if len(filtered) == 0 || filtered[0] == "help" {
		return cliRequest{}, options, "help", nil
	}
	if filtered[0] == "install-cli" {
		return cliRequest{}, options, "install-cli", nil
	}
	if filtered[0] == "version" {
		return cliRequest{Command: "version"}, options, "", nil
	}
	if filtered[0] == "doctor" {
		return cliRequest{Command: "doctor"}, options, "", nil
	}
	if len(filtered) < 2 {
		return cliRequest{}, options, "", fmt.Errorf("%s requires a subcommand", filtered[0])
	}
	group, action, remaining := filtered[0], filtered[1], filtered[2:]
	request := cliRequest{Command: group + "." + action, Wait: options.Wait}
	switch group {
	case "project", "server", "registry":
		if action != "list" || len(remaining) != 0 {
			return cliRequest{}, options, "", fmt.Errorf("supported command: %s list", group)
		}
	case "deployment":
		switch action {
		case "list":
			if len(remaining) != 0 {
				return cliRequest{}, options, "", errors.New("deployment list accepts no arguments")
			}
		case "get", "start", "stop", "restart":
			if len(remaining) != 1 {
				return cliRequest{}, options, "", fmt.Errorf("deployment %s requires a deployment ID", action)
			}
			request.ID = remaining[0]
		case "publish":
			id, flags, err := takeCLIResourceID(remaining, "deployment publish")
			if err != nil {
				return cliRequest{}, options, "", err
			}
			set := newCLIFlagSet("deployment publish")
			set.StringVar(&request.RegistryID, "registry", "", "registry ID")
			set.StringVar(&request.Version, "version", "", "release version")
			if err := set.Parse(flags); err != nil {
				return cliRequest{}, options, "", err
			}
			if set.NArg() != 0 {
				return cliRequest{}, options, "", errors.New("deployment publish received unexpected arguments")
			}
			if request.RegistryID == "" {
				return cliRequest{}, options, "", errors.New("deployment publish requires --registry")
			}
			request.ID = id
		case "rollback":
			id, flags, err := takeCLIResourceID(remaining, "deployment rollback")
			if err != nil {
				return cliRequest{}, options, "", err
			}
			set := newCLIFlagSet("deployment rollback")
			set.StringVar(&request.ReleaseID, "release", "", "release ID")
			if err := set.Parse(flags); err != nil {
				return cliRequest{}, options, "", err
			}
			if set.NArg() != 0 {
				return cliRequest{}, options, "", errors.New("deployment rollback received unexpected arguments")
			}
			if request.ReleaseID == "" {
				return cliRequest{}, options, "", errors.New("deployment rollback requires --release")
			}
			request.ID = id
		default:
			return cliRequest{}, options, "", errors.New("unsupported deployment command")
		}
	case "release":
		if action != "list" {
			return cliRequest{}, options, "", errors.New("supported command: release list --deployment <id>")
		}
		set := newCLIFlagSet("release list")
		set.StringVar(&request.DeploymentID, "deployment", "", "deployment ID")
		if err := set.Parse(remaining); err != nil {
			return cliRequest{}, options, "", err
		}
		if set.NArg() != 0 {
			return cliRequest{}, options, "", errors.New("release list received unexpected arguments")
		}
		if request.DeploymentID == "" {
			return cliRequest{}, options, "", errors.New("release list requires --deployment")
		}
	case "job":
		switch action {
		case "list":
			if len(remaining) != 0 {
				return cliRequest{}, options, "", errors.New("job list accepts no arguments")
			}
		case "get", "wait", "cancel":
			if len(remaining) != 1 {
				return cliRequest{}, options, "", fmt.Errorf("job %s requires a job ID", action)
			}
			request.ID = remaining[0]
		default:
			return cliRequest{}, options, "", errors.New("unsupported job command")
		}
	default:
		return cliRequest{}, options, "", fmt.Errorf("unknown command %q", group)
	}
	return request, options, "", nil
}

func newCLIFlagSet(name string) *flag.FlagSet {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	return set
}

func takeCLIResourceID(arguments []string, command string) (string, []string, error) {
	if len(arguments) == 0 || strings.HasPrefix(arguments[0], "-") {
		return "", nil, fmt.Errorf("%s requires a resource ID before its flags", command)
	}
	return arguments[0], arguments[1:], nil
}

func cliRequestStartsJob(request cliRequest) bool {
	return request.Command == "deployment.publish" || request.Command == "deployment.rollback" ||
		request.Command == "deployment.start" || request.Command == "deployment.stop" || request.Command == "deployment.restart"
}

func cliResponseFromValue(value any) cliResponse {
	encoded, err := json.Marshal(value)
	if err != nil {
		return errorCLIResponse("encoding_failed", err.Error())
	}
	return cliResponse{OK: true, Data: encoded}
}

func writeCLIResult(stdout, stderr io.Writer, options cliOptions, response cliResponse) int {
	if !response.OK {
		if options.JSON {
			_ = json.NewEncoder(stdout).Encode(response)
		} else {
			fmt.Fprintf(stderr, "ideploy: %s\n", response.Error)
		}
		return cliExitCode(response.Code)
	}
	if options.Quiet {
		return 0
	}
	if options.JSON {
		_ = json.NewEncoder(stdout).Encode(response)
		return 0
	}
	return writeHumanCLIResult(stdout, response.Data)
}

func writeHumanCLIResult(stdout io.Writer, data json.RawMessage) int {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	if object, ok := value.(map[string]any); ok {
		if _, hasVersion := object["version"]; hasVersion && len(object) == 3 {
			writer := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
			for _, key := range []string{"version", "platform", "architecture"} {
				if item, exists := object[key]; exists && fmt.Sprint(item) != "" {
					fmt.Fprintf(writer, "%s\t%v\n", key, item)
				}
			}
			_ = writer.Flush()
			return 0
		}
	}
	formatted, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fmt.Fprintln(stdout, string(data))
	} else {
		fmt.Fprintln(stdout, string(formatted))
	}
	return 0
}

func writeCLIError(stdout, stderr io.Writer, jsonOutput bool, code, message string) {
	response := errorCLIResponse(code, message)
	if jsonOutput {
		_ = json.NewEncoder(stdout).Encode(response)
		return
	}
	fmt.Fprintf(stderr, "ideploy: %s\n", message)
}

func cliExitCode(code string) int {
	switch code {
	case "usage", "unknown_command", "invalid_request":
		return 2
	case "not_found":
		return 3
	case "runtime_busy", "background_unavailable":
		return 4
	case "credential_unavailable":
		return 5
	case "job_failed":
		return 7
	case "timeout":
		return 8
	case "canceled", "job_canceled":
		return 130
	default:
		return 1
	}
}

const cliHelp = `iDeploy command line interface

Usage:
  ideploy [global options] <command>

Commands:
  version
  doctor
  install-cli
  mcp                 run the local MCP server over stdio
  project list
  server list
  registry list
  deployment list
  deployment get <deployment-id>
  deployment publish <deployment-id> --registry <registry-id> [--version <version>]
  deployment rollback <deployment-id> --release <release-id>
  deployment start|stop|restart <deployment-id>
  release list --deployment <deployment-id>
  job list|get|wait|cancel [job-id]

Global options:
  --json             write a stable JSON response envelope
  --quiet            suppress successful output
  --wait             wait for an operation job (default)
  --no-wait          return after queuing; requires iDeploy.app to be running
  --timeout <value>  command timeout, for example 30s or 30m
  --no-color         disable colour output
`
