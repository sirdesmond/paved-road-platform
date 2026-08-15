package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"text/tabwriter"
	"time"

	api "github.com/sirdesmond/paved-road-platform/platform-api/pkg/api"
)

func main() {
	if len(os.Args) < 3 || os.Args[1] != "env" {
		usage()
	}

	switch os.Args[2] {
	case "create":
		os.Exit(createCmd(os.Args[3:]))
	case "list":
		os.Exit(listCmd(os.Args[3:]))
	case "status":
		os.Exit(statusCmd(os.Args[3:]))
	default:
		usage()
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `platformctl — request and inspect environments

  platformctl env create --team NAME --tier dev|staging|prod --contact CONTACT
  platformctl env list
  platformctl env status NAME

An environment is an isolated namespace with a resource quota, a default-deny
network policy and an owner. Creating one opens a pull request; merging it
provisions the environment.

  --endpoint   platform-api URL (or $PLATFORM_API)
  -o           text | json
`)
	os.Exit(2)
}

func createCmd(args []string) int {
	fs := flag.NewFlagSet("env create", flag.ExitOnError)
	var req api.CreateEnvironmentRequest
	var endpoint, format string

	fs.StringVar(&req.Team, "team", "", "Owning team.")
	fs.StringVar(&req.Tier, "tier", "dev", "dev, staging or prod.")
	fs.StringVar(&req.Contact, "contact", "", "Slack channel or rota to page.")
	fs.StringVar(&req.Requester, "requester", os.Getenv("USER"), "Who is asking.")
	fs.StringVar(&req.Environment, "name", "", "Override the generated name.")
	fs.StringVar(&req.CPU, "cpu", "", "CPU ceiling, e.g. 2 or 500m. Omit for the tier default.")
	fs.StringVar(&req.Memory, "memory", "", "Memory ceiling, e.g. 4Gi. Omit for the tier default.")
	fs.StringVar(&endpoint, "endpoint", envOr("PLATFORM_API", "http://localhost:8080"), "platform-api base URL.")
	fs.StringVar(&format, "o", "text", "Output format: text or json.")
	_ = fs.Parse(args)

	return create(endpoint, format, req)
}

func create(endpoint, format string, req api.CreateEnvironmentRequest) int {
	body, _ := json.Marshal(req)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(endpoint+"/v1/environments", "application/json", bytes.NewReader(body))
	if err != nil {
		// Distinct exit code: "couldn't reach the platform" is a different
		// problem from "your request was wrong", and a pipeline needs to tell
		// them apart.
		fmt.Fprintf(os.Stderr, "cannot reach platform-api at %s: %v\n", endpoint, err)
		return 3
	}
	defer resp.Body.Close()

	if format == "json" {
		// Pass the API's response through untouched so scripts depend on the
		// API's contract, not on ours.
		var raw json.RawMessage
		_ = json.NewDecoder(resp.Body).Decode(&raw)
		fmt.Println(string(raw))
		return exitFor(resp.StatusCode)
	}

	switch resp.StatusCode {
	case http.StatusAccepted:
		var out api.CreateEnvironmentResponse
		_ = json.NewDecoder(resp.Body).Decode(&out)
		fmt.Printf("%s requested\n  %s\n  %s\n", out.Name, out.PullRequest, out.Message)
		return 0

	case http.StatusUnprocessableEntity:
		var out struct {
			Errors []api.ValidationError `json:"errors"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		for _, e := range out.Errors {
			// Field first, then the explanation indented. Nobody reads a JSON
			// dump, and this is the whole perceived quality of the tool.
			fmt.Fprintf(os.Stderr, "✗ %s\n    %s\n", e.Field, e.Message)
		}
		return 1

	default:
		var out map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&out)
		fmt.Fprintf(os.Stderr, "✗ %s\n", out["error"])
		return exitFor(resp.StatusCode)
	}
}

func listCmd(args []string) int {
	fs := flag.NewFlagSet("env list", flag.ExitOnError)
	endpoint := fs.String("endpoint", envOr("PLATFORM_API", "http://localhost:8080"), "platform-api base URL.")
	format := fs.String("o", "text", "Output format: text or json.")
	_ = fs.Parse(args)

	body, status, err := get(*endpoint + "/v1/environments")
	if err != nil {
		return unreachable(*endpoint, err)
	}
	if *format == "json" {
		fmt.Println(string(body))
		return exitFor(status)
	}

	var envs []api.EnvironmentSummary
	if err := json.Unmarshal(body, &envs); err != nil {
		fmt.Fprintf(os.Stderr, "unexpected response: %s\n", body)
		return 4
	}
	if len(envs) == 0 {
		fmt.Println("No environments yet. Create one with: platformctl env create --team NAME --tier dev")
		return 0
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tTEAM\tTIER\tREADY\tEXPIRES")
	for _, e := range envs {
		ready := "no"
		if e.Ready {
			ready = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", e.Name, e.Team, e.Tier, ready, dash(e.ExpiresAt))
	}
	w.Flush()
	return 0
}

func statusCmd(args []string) int {
	fs := flag.NewFlagSet("env status", flag.ExitOnError)
	endpoint := fs.String("endpoint", envOr("PLATFORM_API", "http://localhost:8080"), "platform-api base URL.")
	format := fs.String("o", "text", "Output format: text or json.")
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: platformctl env status NAME")
		return 2
	}
	name := fs.Arg(0)

	body, status, err := get(*endpoint + "/v1/environments/" + name)
	if err != nil {
		return unreachable(*endpoint, err)
	}
	if *format == "json" {
		fmt.Println(string(body))
		return exitFor(status)
	}

	var st api.EnvironmentStatus
	if err := json.Unmarshal(body, &st); err != nil {
		fmt.Fprintf(os.Stderr, "unexpected response: %s\n", body)
		return 4
	}

	// Lead with the phase. It's the answer to the question they asked.
	fmt.Printf("%s: %s\n", st.Name, st.Phase)
	if st.Message != "" {
		fmt.Printf("  %s\n", st.Message)
	}
	if st.PullRequest != "" {
		fmt.Printf("  %s\n", st.PullRequest)
	}
	if st.Namespace != "" {
		fmt.Printf("  namespace: %s\n", st.Namespace)
	}
	if st.ExpiresAt != "" {
		fmt.Printf("  expires:   %s\n", st.ExpiresAt)
	}
	return exitFor(status)
}

func get(url string) ([]byte, int, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	return buf.Bytes(), resp.StatusCode, nil
}

func unreachable(endpoint string, err error) int {
	fmt.Fprintf(os.Stderr, "cannot reach platform-api at %s: %v\n", endpoint, err)
	return 3
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func exitFor(status int) int {
	switch {
	case status < 300:
		return 0
	case status < 500:
		return 1 // your request
	default:
		return 4 // theirs
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
