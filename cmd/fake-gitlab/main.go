// Command fake-gitlab serves the fake GitLab API as a standalone process, for
// the browser end-to-end suite (the Go suite mounts the same handler in
// process). It is a test fixture and has no place in a deployment.
//
//	go run ./cmd/fake-gitlab -addr 127.0.0.1:8081 -token glpat-e2e
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/paulozy/idp-with-ai-backend/internal/testsupport/fakegitlab"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8081", "address to listen on")
	token := flag.String("token", "glpat-e2e-token", "bearer token the fake requires")
	flag.Parse()

	server := fakegitlab.Default(*token)
	fmt.Printf("fake gitlab listening on http://%s (projects: %s, %s)\n", *addr, fakegitlab.RunnerPath, fakegitlab.HugePath)
	if err := http.ListenAndServe(*addr, server.Handler()); err != nil {
		fmt.Fprintln(os.Stderr, "fake-gitlab:", err)
		os.Exit(1)
	}
}
